package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func TestWorkerStopsOnRunCheckpointFailureWithoutCompletingJob(t *testing.T) {
	for _, phase := range []string{"attempt start", "attempt outcome", "completion"} {
		t.Run(phase, func(t *testing.T) {
			var calls atomic.Int32
			node := workflow.Node{
				Name: "probe", Version: 1,
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Resources:   func(json.RawMessage) ([]string, error) { return nil, nil },
				Execute: func(context.Context, json.RawMessage) (string, error) {
					calls.Add(1)
					return `{}`, nil
				},
			}
			db, svc := poolService(t, node)
			writer, err := db.NewRun()
			if err != nil {
				t.Fatal(err)
			}
			path := writer.DatabasePath()
			err = writer.Close()
			if err != nil {
				t.Fatal(err)
			}
			sqlite, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer sqlite.Close()
			trigger := `CREATE TRIGGER reject_checkpoint BEFORE UPDATE ON runs WHEN NEW.status='completed'`
			if phase == "attempt start" {
				trigger = `CREATE TRIGGER reject_checkpoint BEFORE INSERT ON attempts WHEN json_extract(CAST(NEW.record AS TEXT), '$.finished') IS NULL`
			}
			if phase == "attempt outcome" {
				trigger = `CREATE TRIGGER reject_checkpoint BEFORE INSERT ON attempts WHEN json_extract(CAST(NEW.record AS TEXT), '$.finished') IS NOT NULL`
			}
			_, err = sqlite.Exec(trigger + ` BEGIN SELECT RAISE(ABORT, 'run checkpoint fault'); END`)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				_, err = db.Enqueue(t.Context(), "probe.json", json.RawMessage(`{"version":4,"name":"Checkpoint","steps":[{"id":"probe","tool":"probe","args":{}}]}`), nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err = Serve(ctx, db, svc, Config{})
			wantCalls := int32(1)
			if phase == "attempt start" {
				wantCalls = 0
			}
			if err == nil || !strings.Contains(err.Error(), "run checkpoint fault") || calls.Load() != wantCalls {
				t.Fatal("checkpoint failure did not stop queued work", err, calls.Load())
			}
			jobs, err := db.Jobs(t.Context())
			if err != nil || len(jobs) != 2 || jobs[0].Status != "interrupted" || jobs[1].Status != "queued" {
				t.Fatal("uncommitted result was reported as completed", jobs, err)
			}
			state, err := db.Run(jobs[0].RunID)
			if err != nil || state.Status != "interrupted" || state.Record.Status != "running" {
				t.Fatal("run lost its recovery state", state, err)
			}
			_, err = sqlite.Exec("DROP TRIGGER reject_checkpoint")
			if err != nil {
				t.Fatal(err)
			}
			request := workflow.Recovery{Note: "Inspect the last durable checkpoint before continuing"}
			for _, step := range workflow.UncertainSteps(state.Record) {
				decision := workflow.RecoveryDecision{StepID: step.ID, Action: "complete", Output: json.RawMessage(`{}`)}
				if phase == "attempt start" {
					decision.Action, decision.Output = "retry", nil
				}
				request.Decisions = append(request.Decisions, decision)
			}
			_, err = svc.Recover(t.Context(), jobs[0].RunID, request)
			if err != nil || calls.Load() != 1 {
				t.Fatal("explicit recovery lost or repeated an effect", err, calls.Load())
			}
			job, err := db.Job(t.Context(), jobs[0].ID)
			if err != nil || job.Status != "completed" {
				t.Fatal("recovered job was not reconciled", job, err)
			}
		})
	}
}

func TestCheckpointFailureCancelsAndJoinsPeerJobsBeforeReleasingLease(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	node := workflow.Node{
		Name: "probe", Version: 1, Concurrent: true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}},"required":["label"]}`),
		Resources:   func(json.RawMessage) ([]string, error) { return nil, nil },
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var in struct{ Label string }
			err := json.Unmarshal(raw, &in)
			if err != nil {
				return "", err
			}
			calls.Add(1)
			if in.Label == "blocked" {
				close(started)
				<-ctx.Done()
				close(cancelled)
				<-release // Simulate cleanup that must finish before the lease is released.
			} else {
				<-started
			}
			return `{}`, nil
		},
	}
	db, svc := poolService(t, node)
	writer, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	path := writer.DatabasePath()
	err = writer.Close()
	if err != nil {
		t.Fatal(err)
	}
	sqlite, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlite.Close()
	_, err = sqlite.Exec(`CREATE TRIGGER reject_checkpoint BEFORE UPDATE ON runs
WHEN NEW.status='completed' AND json_extract(CAST(NEW.record AS TEXT), '$.definition.name')='fault'
BEGIN SELECT RAISE(ABORT, 'run checkpoint fault'); END`)
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"blocked", "fault", "queued"} {
		args, encodeErr := json.Marshal(struct {
			Label string `json:"label"`
		}{Label: label})
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		definition := workflow.Definition{Version: 4, Name: label, Steps: []workflow.Step{{ID: "probe", Tool: "probe", Args: args}}}
		source, encodeErr := json.Marshal(definition)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		_, err = db.Enqueue(t.Context(), label+".json", source, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	var once sync.Once
	joined := false
	t.Cleanup(func() {
		cancel()
		once.Do(func() { close(release) })
		if !joined {
			<-done
		}
	})
	go func() { done <- Serve(ctx, db, svc, Config{MaxJobs: 2, MaxOperations: 2}) }()
	select {
	case <-cancelled:
	case err = <-done:
		joined = true
		t.Fatal("worker returned before peer cleanup", err)
	case <-time.After(5 * time.Second):
		t.Fatal("checkpoint failure did not cancel its peer")
	}
	held, err := db.WorkerLeaseHeld()
	if err != nil || !held || calls.Load() != 2 {
		t.Fatal("lease released or new work dispatched during cleanup", held, calls.Load(), err)
	}
	select {
	case err = <-done:
		joined = true
		t.Fatal("worker returned with peer cleanup pending", err)
	default:
	}
	once.Do(func() { close(release) })
	select {
	case err = <-done:
		joined = true
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not join peer cleanup")
	}
	if err == nil || !strings.Contains(err.Error(), "run checkpoint fault") {
		t.Fatal("peer cancellation hid the persistence error", err)
	}
	held, err = db.WorkerLeaseHeld()
	if err != nil || held {
		t.Fatal("worker retained lease after joining", held, err)
	}
	jobs, err := db.Jobs(t.Context())
	if err != nil || jobs[0].Status != "cancelled" || jobs[1].Status != "interrupted" || jobs[2].Status != "queued" {
		t.Fatal("incorrect peer shutdown records", jobs, err)
	}
}
