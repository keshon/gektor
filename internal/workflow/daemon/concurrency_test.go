package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func poolService(t *testing.T, node workflow.Node) (*store.Store, service.Service) {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	directory := t.TempDir()
	db, err := store.OpenStore(directory, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	registry, err := workflow.NewRegistry(node)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.Service{
		WS:                ws,
		StoreDir:          directory,
		Registry:          registry,
		AuthorizeResource: func(context.Context, string, string, json.RawMessage) error { return nil },
	}
	return db, svc
}

func TestWorkerBoundsJobsAndOperationsAndJoinsBeforeReleasingLease(t *testing.T) {
	for _, test := range []struct {
		name                     string
		jobs, operations, active int
		concurrent               bool
	}{
		{name: "job slots", jobs: 2, operations: 32, active: 2, concurrent: true},
		{name: "operation slots", jobs: 4, operations: 2, active: 2, concurrent: true},
		{name: "serial operation", jobs: 4, operations: 32, active: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{}, 6)
			release := make(chan struct{})
			var active, peak atomic.Int32
			node := workflow.Node{
				Name:        "probe",
				Version:     1,
				Concurrent:  test.concurrent,
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Resources:   func(json.RawMessage) ([]string, error) { return nil, nil },
				Execute: func(context.Context, json.RawMessage) (string, error) {
					count := active.Add(1)
					defer active.Add(-1)
					for previous := peak.Load(); count > previous; previous = peak.Load() {
						if peak.CompareAndSwap(previous, count) {
							break
						}
					}
					started <- struct{}{}
					<-release // Deliberately ignore cancellation until the owner releases the operation.
					return `{}`, nil
				},
			}
			db, svc := poolService(t, node)
			for range 6 {
				_, err := db.Enqueue(t.Context(), "probe.json", json.RawMessage(`{"version":4,"name":"Pool","steps":[{"id":"probe","tool":"probe","args":{}}]}`), nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			var releaseOnce sync.Once
			var joined bool
			t.Cleanup(func() {
				cancel()
				releaseOnce.Do(func() { close(release) })
				if !joined {
					<-done
				}
			})
			go func() { done <- Serve(ctx, db, svc, Config{MaxJobs: test.jobs, MaxOperations: test.operations}) }()
			for range test.active {
				select {
				case <-started:
				case err := <-done:
					joined = true
					t.Fatal("worker stopped before filling slots", err)
				case <-time.After(10 * time.Second):
					t.Fatal("worker did not fill execution slots")
				}
			}
			select {
			case <-started:
				t.Fatal("worker exceeded configured capacity")
			case <-time.After(50 * time.Millisecond):
			}
			cancel()
			held, err := db.WorkerLeaseHeld()
			if err != nil || !held {
				t.Fatal("lease released before operations stopped", held, err)
			}
			select {
			case err := <-done:
				joined = true
				t.Fatal("worker returned with active operations", err)
			default:
			}
			releaseOnce.Do(func() { close(release) })
			select {
			case err = <-done:
				joined = true
			case <-time.After(10 * time.Second):
				t.Fatal("worker did not join operations")
			}
			if !errors.Is(err, context.Canceled) || active.Load() != 0 || peak.Load() != int32(test.active) {
				t.Fatal("incorrect shutdown or execution bound", err, active.Load(), peak.Load())
			}
			held, err = db.WorkerLeaseHeld()
			if err != nil || held {
				t.Fatal("worker lease retained after shutdown", held, err)
			}
			jobs, err := db.Jobs(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			claimed := 0
			for _, job := range jobs {
				if job.Status == "running" {
					t.Fatal("claimed job left running after shutdown", job.ID)
				}
				if job.RunID != "" {
					claimed++
				}
			}
			if claimed > test.jobs {
				t.Fatal("worker claimed beyond capacity", claimed, test.jobs)
			}
		})
	}
}

func TestInvalidWorkerLimitsDoNotTakeLease(t *testing.T) {
	node := workflow.Node{
		Name: "probe", Version: 1,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Resources:   func(json.RawMessage) ([]string, error) { return nil, nil },
		Execute:     func(context.Context, json.RawMessage) (string, error) { return `{}`, nil },
	}
	db, svc := poolService(t, node)
	for _, cfg := range []Config{{MaxJobs: -1}, {MaxJobs: MaxJobs + 1}, {MaxOperations: -1}, {MaxOperations: workflow.MaxConcurrency + 1}} {
		err := Serve(t.Context(), db, svc, cfg)
		if err == nil {
			t.Fatal("invalid worker limit accepted", cfg)
		}
		held, leaseErr := db.WorkerLeaseHeld()
		if leaseErr != nil || held {
			t.Fatal("invalid configuration retained lease", cfg, held, leaseErr)
		}
	}
}

func TestWorkerDrainsQueueWithoutRepeatingJobsAfterAnOperationFailure(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	node := workflow.Node{
		Name:        "probe",
		Version:     1,
		Concurrent:  true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}},"required":["label"]}`),
		Resources:   func(json.RawMessage) ([]string, error) { return nil, nil },
		Execute: func(_ context.Context, raw json.RawMessage) (string, error) {
			var in struct{ Label string }
			err := json.Unmarshal(raw, &in)
			if err != nil {
				return "", err
			}
			mu.Lock()
			calls[in.Label]++
			mu.Unlock()
			if in.Label == "fail" {
				return "", &workflow.ExecutionError{Code: "probe_failed", Err: errors.New("expected operation failure")}
			}
			return `{}`, nil
		},
	}
	db, svc := poolService(t, node)
	labels := []string{"fail", "a", "b", "c", "d", "e"}
	for _, label := range labels {
		args, err := json.Marshal(struct {
			Label string `json:"label"`
		}{Label: label})
		if err != nil {
			t.Fatal(err)
		}
		definition := workflow.Definition{
			Version: 4,
			Name:    label,
			Steps:   []workflow.Step{{ID: "probe", Tool: "probe", Args: args}},
		}
		source, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Enqueue(t.Context(), label+".json", source, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	joined := false
	t.Cleanup(func() {
		cancel()
		if !joined {
			<-done
		}
	})
	go func() { done <- Serve(ctx, db, svc, Config{MaxJobs: 3, MaxOperations: 2}) }()
	deadline := time.After(10 * time.Second)
	for {
		jobs, err := db.Jobs(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		complete, failed := 0, 0
		for _, job := range jobs {
			if job.Status == "completed" {
				complete++
			}
			if job.Status == "failed" {
				failed++
			}
		}
		if complete == len(labels)-1 && failed == 1 {
			break
		}
		select {
		case err = <-done:
			joined = true
			t.Fatal("operation failure stopped the worker", err)
		case <-deadline:
			t.Fatal("queue did not drain", jobs)
		case <-time.After(20 * time.Millisecond):
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for _, label := range labels {
		if calls[label] != 1 {
			t.Fatal("job was lost or repeated", label, calls[label])
		}
	}
}
