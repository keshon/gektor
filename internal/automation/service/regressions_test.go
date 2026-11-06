package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/workspace"
	"testing"
	"time"
)

func TestIterationCannotBorrowStepAuthorization(t *testing.T) {
	ws, e := workspace.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer ws.Close()
	calls := 0
	n := automation.Node{Name: "echo", Version: 1, InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`), Resources: func(raw json.RawMessage) ([]string, error) {
		var a struct{ Path string }
		e := json.Unmarshal(raw, &a)
		return []string{a.Path}, e
	}, Execute: func(_ context.Context, raw json.RawMessage) (string, error) { calls++; return string(raw), nil }}
	reg, e := automation.NewRegistry(n)
	if e != nil {
		t.Fatal(e)
	}
	svc := Service{WS: ws, StoreDir: t.TempDir(), Registry: reg, AuthorizeResource: func(_ context.Context, _, r string, _ json.RawMessage) error {
		if r != "allowed" {
			return fmt.Errorf("denied")
		}
		return nil
	}}
	_, e = svc.RunSource(t.Context(), []byte(`{"version":3,"name":"collision","steps":[{"id":"loop[0]","tool":"echo","args":{"path":"allowed"}},{"id":"loop","tool":"echo","args":{},"for_each":[{"path":"denied"}]}]}`), "r.json", nil)
	if e == nil || calls != 1 {
		t.Fatalf("permission bypass: %v calls=%d", e, calls)
	}
}
func pendingJob(t *testing.T, execute func(context.Context, json.RawMessage) (string, error)) (Service, *store.Store, store.Job, automation.Node) {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	node := automation.Node{Name: "echo", Version: 1, InputSchema: json.RawMessage(`{"type":"object"}`), Resources: func(json.RawMessage) ([]string, error) { return nil, nil }, Execute: execute}
	reg, err := automation.NewRegistry(node)
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{WS: ws, StoreDir: t.TempDir(), Registry: reg, AuthorizeResource: func(context.Context, string, string, json.RawMessage) error { return nil }}
	db, err := store.OpenStore(svc.StoreDir, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	j, err := db.Enqueue(t.Context(), "r.json", json.RawMessage(`{"version":3,"name":"review","steps":[{"id":"review","tool":"echo","review":true,"args":{}}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	j, _, err = db.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ExecuteJob(t.Context(), db, j); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.Jobs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return svc, db, jobs[0], node
}
func TestQueuedReviewUsesCancellationAndRecovery(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(fmt.Sprint(external), func(t *testing.T) {
			started := make(chan struct{})
			svc, db, job, _ := pendingJob(t, func(ctx context.Context, _ json.RawMessage) (string, error) {
				close(started)
				<-ctx.Done()
				return "", ctx.Err()
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := svc.Review(ctx, job.RunID, "review", true); done <- err }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("not started")
			}
			if err := db.RecoverJobs(t.Context()); err != nil {
				t.Fatal(err)
			}
			jobs, _ := db.Jobs(t.Context())
			if jobs[0].Status != "running" {
				t.Fatal(jobs)
			}
			if external {
				cancel()
			} else if err := db.CancelJob(t.Context(), job.ID); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("missing cancellation")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("did not cancel")
			}
			jobs, _ = db.Jobs(t.Context())
			if jobs[0].Status != "cancelled" {
				t.Fatal(jobs)
			}
		})
	}
}
func TestFailedReviewValidationPreservesJob(t *testing.T) {
	svc, db, job, node := pendingJob(t, func(context.Context, json.RawMessage) (string, error) { return `{}`, nil })
	node.Version = 2
	svc.Registry, _ = automation.NewRegistry(node)
	if _, err := svc.Review(t.Context(), job.RunID, "review", true); err == nil {
		t.Fatal("version mismatch accepted")
	}
	jobs, err := db.Jobs(t.Context())
	if err != nil || jobs[0].Status != "waiting_review" {
		t.Fatal(jobs, err)
	}
	run, err := db.Run(job.RunID)
	if err != nil || run.Status != "waiting_review" {
		t.Fatal(run, err)
	}
}

func TestRecoveryReconcilesAbandonedContinuation(t *testing.T) {
	svc, db, job, _ := pendingJob(t, func(context.Context, json.RawMessage) (string, error) { return `{}`, nil })
	_ = svc
	writer, run, err := db.EditRun(job.RunID)
	if err != nil {
		t.Fatal(err)
	}
	run.Status = "running"
	if err := writer.Save(run); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateJob(t.Context(), job.ID, func(j *store.Job) error { j.Status = "running"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := db.RecoverJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	jobs, _ := db.Jobs(t.Context())
	if jobs[0].Status != "running" {
		t.Fatal("live owner interrupted")
	}
	writer.Close()
	if err := db.ReconcileRuns(t.Context()); err != nil {
		t.Fatal(err)
	}
	jobs, _ = db.Jobs(t.Context())
	if jobs[0].Status != "interrupted" {
		t.Fatal(jobs)
	}
}
