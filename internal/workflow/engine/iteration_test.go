package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	r, err := Run(ctx, collectionDefinition(t, 6, 3), h, func(r workflow.Run) error {
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
	r, err := Run(t.Context(), collectionDefinition(t, 500, 8), h, func(workflow.Run) error { return nil })
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
			r, err := Run(t.Context(), collectionDefinition(t, 3, 1), h, func(workflow.Run) error { return nil })
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
	_, err := Run(t.Context(), collectionDefinition(t, 8, 4), h, func(r workflow.Run) error {
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
	r, err := Run(t.Context(), d, h, func(r workflow.Run) error {
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
	r, err := Run(t.Context(), d, h, func(workflow.Run) error { return nil })
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
		r, _ := Run(ctx, d, h, func(workflow.Run) error { return nil })
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
	r, err := Run(t.Context(), collectionDefinition(t, 0, 4), h, func(workflow.Run) error { return nil })
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
			r, err := Run(t.Context(), d, h, func(workflow.Run) error { return nil })
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
