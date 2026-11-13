package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

type collectionHandler struct {
	execute func(context.Context, workflow.Step) (string, error)
}

func (h collectionHandler) Validate(workflow.Step) error { return nil }
func (h collectionHandler) Run(ctx context.Context, s workflow.Step) (string, error) {
	return h.execute(ctx, s)
}

func collectionDefinition(t *testing.T, count, concurrency int) workflow.Definition {
	t.Helper()
	items := make([]map[string]int, count)
	for n := range items {
		items[n] = map[string]int{"value": n}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	return workflow.Definition{Version: 4, Name: "Collection", Steps: []workflow.Step{
		{ID: "scan", Args: json.RawMessage(`{}`), ForEach: raw, Concurrency: concurrency, OnError: "collect"},
	}}
}

func budgetCheckpoint(available int) workflow.Run {
	r := workflow.Run{Steps: make([]workflow.StepResult, 11)}
	remaining := workflow.MaxInvocations - len(r.Steps) - available
	for i := range 10 {
		count := min(remaining, workflow.MaxIterations)
		r.Steps[i] = workflow.StepResult{
			ID: fmt.Sprintf("prior%d", i), Status: "completed",
			Iterations: make([]workflow.StepResult, count),
		}
		remaining -= count
	}
	r.Steps[10] = workflow.StepResult{ID: "scan", Status: "running"}
	return r
}

func TestCollectionReservesRunBudgetBeforeWorkers(t *testing.T) {
	for _, tc := range []struct{ available, count int }{{0, 0}, {0, 3}, {2, 3}, {3, 3}} {
		t.Run(fmt.Sprintf("available%d_items%d", tc.available, tc.count), func(t *testing.T) {
			r := budgetCheckpoint(tc.available)
			r.Steps[0].Iterations[0].Status = "failed"
			r.Steps[0].Iterations[1].Status = "skipped"
			d := collectionDefinition(t, tc.count, 3)
			var calls atomic.Int32
			h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) {
				calls.Add(1)
				return `true`, nil
			}}
			saves := 0
			remaining := workflow.MaxInvocations - invocationCount(r)
			_, err := executeIterations(t.Context(), d.Steps[0], h, &r, 10, func(workflow.Run, ...int) error {
				saves++
				return nil
			}, &remaining)
			if tc.count <= tc.available {
				if err != nil || int(calls.Load()) != tc.count || len(r.Steps[10].Iterations) != tc.count || saves == 0 {
					t.Fatalf("exact fit: calls=%d saves=%d err=%v", calls.Load(), saves, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "invocation slots") || calls.Load() != 0 || saves != 0 || len(r.Steps[10].Iterations) != 0 {
				t.Fatalf("budget overrun: calls=%d saves=%d err=%v", calls.Load(), saves, err)
			}
		})
	}
}

func TestCollectionRetryReusesInvocationSlot(t *testing.T) {
	r := budgetCheckpoint(1)
	d := collectionDefinition(t, 1, 1)
	d.Steps[0].Retry = &workflow.RetryPolicy{MaxAttempts: 2}
	calls := 0
	h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) {
		calls++
		if calls == 1 {
			return "", &workflow.ExecutionError{Err: errors.New("temporary")}
		}
		return `true`, nil
	}}
	remaining := workflow.MaxInvocations - invocationCount(r)
	_, err := executeIterations(t.Context(), d.Steps[0], h, &r, 10, func(workflow.Run, ...int) error { return nil }, &remaining)
	if err != nil || calls != 2 || len(r.Steps[10].Iterations[0].Attempts) != 2 {
		t.Fatalf("retry at budget: calls=%d err=%v", calls, err)
	}
}

func TestReviewContinuationRetainsCollectionBudget(t *testing.T) {
	d := collectionDefinition(t, 3, 2)
	d.Steps[0].Review = true
	checkpoint := budgetCheckpoint(2)
	for i := range 10 {
		count := len(checkpoint.Steps[i].Iterations)
		args := "[" + strings.TrimSuffix(strings.Repeat("{},", count), ",") + "]"
		d.Steps = append(d.Steps, workflow.Step{
			ID: checkpoint.Steps[i].ID, Args: json.RawMessage(`{}`), ForEach: json.RawMessage(args),
			Review: true,
		})
	}
	// The authored order determines checkpoint order. Prior collections stand for
	// work completed before the review, with the same frozen definition digest.
	d.Steps = append(d.Steps[1:], d.Steps[0])
	calls := 0
	h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) {
		calls++
		return `true`, nil
	}}
	r, err := Run(t.Context(), d, h, func(workflow.Run, ...int) error { return nil })
	if err != nil || r.Status != "waiting_review" || calls != 0 {
		t.Fatal("review boundary", r.Status, calls, err)
	}
	for i := range 10 {
		r.Steps[i] = checkpoint.Steps[i]
	}
	r.Steps[10].Status = "waiting_review"
	r.Approved = map[string]bool{"scan": true}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var restored workflow.Run
	err = json.Unmarshal(encoded, &restored)
	if err != nil {
		t.Fatal(err)
	}
	r, err = Resume(t.Context(), restored, h, func(workflow.Run, ...int) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "invocation slots") || calls != 0 || r.Status != "failed" || r.Steps[10].Status != "failed" {
		t.Fatalf("review reset budget: status=%s calls=%d err=%v", r.Status, calls, err)
	}
}

func TestCollectionOverlapsWorkersAndRetainsInputOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, 3)
	thirdSaved := make(chan struct{})
	var release sync.Once
	var active, peak atomic.Int32
	h := collectionHandler{execute: func(ctx context.Context, s workflow.Step) (string, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); current > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, current) {
				break
			}
		}
		var in struct{ Value int }
		err := json.Unmarshal(s.Args, &in)
		if err != nil {
			return "", err
		}
		if in.Value < 3 {
			started <- struct{}{}
		}
		if in.Value != 2 {
			select {
			case <-thirdSaved:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		} else {
			for range 3 {
				select {
				case <-started:
				case <-ctx.Done():
					return "", ctx.Err()
				}
			}
		}
		return string(s.Args), nil
	}}
	r, err := Run(ctx, collectionDefinition(t, 6, 3), h, func(r workflow.Run, _ ...int) error {
		items := r.Steps[0].Iterations
		if len(items) == 6 && items[2].Status == "completed" {
			release.Do(func() { close(thirdSaved) })
		}
		return nil
	})
	if err != nil || peak.Load() != 3 || active.Load() != 0 {
		t.Fatalf("overlap/join: peak=%d active=%d err=%v", peak.Load(), active.Load(), err)
	}
	var output struct{ Items []iterationOutcome }
	err = json.Unmarshal([]byte(r.Steps[0].Output), &output)
	if err != nil || len(output.Items) != 6 {
		t.Fatal(output, err)
	}
	for n, item := range output.Items {
		if item.Index != n || item.Status != "completed" || string(item.Output) != fmt.Sprintf(`{"value":%d}`, n) {
			t.Fatalf("item %d: %+v", n, item)
		}
	}
}

func TestCollectionAccountsForFiveHundredMixedResults(t *testing.T) {
	var calls atomic.Int32
	h := collectionHandler{execute: func(_ context.Context, s workflow.Step) (string, error) {
		calls.Add(1)
		var in struct{ Value int }
		err := json.Unmarshal(s.Args, &in)
		if err != nil {
			return "", err
		}
		if in.Value%7 == 0 {
			return string(s.Args), &workflow.ExecutionError{Err: errors.New("site unavailable")}
		}
		return string(s.Args), nil
	}}
	r, err := Run(t.Context(), collectionDefinition(t, 500, 8), h, func(workflow.Run, ...int) error { return nil })
	if err != nil || r.Status != "completed" || calls.Load() != 500 {
		t.Fatalf("scan: status=%s calls=%d err=%v", r.Status, calls.Load(), err)
	}
	var output struct{ Items []iterationOutcome }
	err = json.Unmarshal([]byte(r.Steps[0].Output), &output)
	if err != nil || len(output.Items) != 500 {
		t.Fatal("incomplete report", len(output.Items), err)
	}
	for n, item := range output.Items {
		status := "completed"
		if n%7 == 0 {
			status = "failed"
			if item.Error != "site unavailable" {
				t.Fatal("lost failure", n, item)
			}
		}
		if item.Index != n || item.Status != status || string(item.Output) != fmt.Sprintf(`{"value":%d}`, n) {
			t.Fatal("wrong item attribution", n, item)
		}
	}
}

func TestCollectionDoesNotCollectControlFailures(t *testing.T) {
	for _, kind := range []string{"authorization", "output"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) {
				calls++
				if kind == "authorization" {
					return "", errors.New("denied")
				}
				return "not JSON", nil
			}}
			r, err := Run(t.Context(), collectionDefinition(t, 3, 1), h, func(workflow.Run, ...int) error { return nil })
			if err == nil || r.Status != "failed" || calls != 1 || r.Steps[0].Iterations[1].Status != "skipped" {
				t.Fatal("control failure collected", r, calls, err)
			}
		})
	}
}

func TestCollectionPersistenceFailureStopsBeforeEffects(t *testing.T) {
	var calls atomic.Int32
	disk := errors.New("disk unavailable")
	h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) {
		calls.Add(1)
		return `{}`, nil
	}}
	_, err := Run(t.Context(), collectionDefinition(t, 8, 4), h, func(r workflow.Run, _ ...int) error {
		for _, item := range r.Steps[0].Iterations {
			if len(item.Attempts) != 0 {
				return disk
			}
		}
		return nil
	})
	if !errors.Is(err, disk) || calls.Load() != 0 {
		t.Fatal("executed without checkpoint", calls.Load(), err)
	}
}

func TestCollectionOutcomeCheckpointFailurePreventsRetryAndDispatch(t *testing.T) {
	var calls atomic.Int32
	disk := errors.New("outcome could not be saved")
	h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) {
		calls.Add(1)
		return `{}`, &workflow.ExecutionError{Err: errors.New("site unavailable")}
	}}
	d := collectionDefinition(t, 4, 1)
	d.Steps[0].Retry = &workflow.RetryPolicy{MaxAttempts: 3}
	r, err := Run(t.Context(), d, h, func(r workflow.Run, _ ...int) error {
		for _, item := range r.Steps[0].Iterations {
			if len(item.Attempts) > 0 && item.Attempts[0].Finished != nil {
				return disk
			}
		}
		return nil
	})
	if !errors.Is(err, disk) || calls.Load() != 1 || r.Steps[0].Iterations[1].Status != "skipped" {
		t.Fatal("retried or dispatched after losing outcome", calls.Load(), err)
	}
}

func TestCollectionStopsOnUncollectedOperationFailure(t *testing.T) {
	calls := 0
	h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) {
		calls++
		return `{}`, &workflow.ExecutionError{Err: errors.New("site unavailable")}
	}}
	d := collectionDefinition(t, 4, 1)
	d.Steps[0].OnError = ""
	r, err := Run(t.Context(), d, h, func(workflow.Run, ...int) error { return nil })
	if err == nil || calls != 1 || r.Status != "failed" || r.Steps[0].Iterations[1].Status != "skipped" {
		t.Fatal("unrequested failure collection", calls, r, err)
	}
}

func TestCollectionCancellationJoinsStartedWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, 4)
	var active, calls atomic.Int32
	h := collectionHandler{execute: func(ctx context.Context, _ workflow.Step) (string, error) {
		calls.Add(1)
		active.Add(1)
		defer active.Add(-1)
		started <- struct{}{}
		<-ctx.Done()
		return "", &workflow.ExecutionError{Err: ctx.Err()}
	}}
	d := collectionDefinition(t, 20, 4)
	done := make(chan workflow.Run, 1)
	go func() {
		r, _ := Run(ctx, d, h, func(workflow.Run, ...int) error { return nil })
		done <- r
	}()
	for range 4 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("workers did not start")
		}
	}
	cancel()
	r := <-done
	if r.Status != "cancelled" || active.Load() != 0 || calls.Load() != 4 {
		t.Fatal("workers not joined", r.Status, active.Load(), calls.Load())
	}
	for n, item := range r.Steps[0].Iterations {
		status := "skipped"
		if n < 4 {
			status = "cancelled"
		}
		if item.Status != status {
			t.Fatal(n, item.Status)
		}
	}
}

func TestEmptyCollectionProducesEmptyItems(t *testing.T) {
	h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) {
		t.Error("empty map executed an item")
		return `{}`, nil
	}}
	r, err := Run(t.Context(), collectionDefinition(t, 0, 4), h, func(workflow.Run, ...int) error { return nil })
	if err != nil || r.Steps[0].Output != `{"items":[]}` {
		t.Fatal(r, err)
	}
}

func TestCollectionRetriesOnlyOperationFailures(t *testing.T) {
	for _, operation := range []bool{false, true} {
		t.Run(fmt.Sprint(operation), func(t *testing.T) {
			calls := 0
			h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) {
				calls++
				err := errors.New("unavailable")
				if operation {
					return "", &workflow.ExecutionError{Err: err}
				}
				return "", err
			}}
			d := collectionDefinition(t, 1, 1)
			d.Steps[0].Retry = &workflow.RetryPolicy{MaxAttempts: 3}
			r, err := Run(t.Context(), d, h, func(workflow.Run, ...int) error { return nil })
			if operation {
				if err != nil || calls != 3 || len(r.Steps[0].Iterations[0].Attempts) != 3 {
					t.Fatal("operation retry lost", calls, err)
				}
			} else if err == nil || calls != 1 {
				t.Fatal("control failure retried", calls, err)
			}
		})
	}
}
