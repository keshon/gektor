package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
)

func TestTypedFailuresRouteBeforeFinalizersAndRemainFailed(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			d := workflow.Definition{Version: 4, Name: "Failure routes", Steps: []workflow.Step{
				{ID: "finish", Tool: "probe", Finally: true, Args: json.RawMessage(`{}`)},
				{ID: "after", Tool: "probe", DependsOn: []string{"fetch"}, Args: json.RawMessage(`{}`)},
				{ID: "report", Tool: "probe", OnFailure: "fetch", Args: json.RawMessage(`{"code":{"$step":"fetch","pointer":"/error/code"}}`)},
				{ID: "fetch", Tool: "probe", Args: json.RawMessage(`{}`)},
			}}
			var calls []string
			h := collectionHandler{execute: func(_ context.Context, step workflow.Step) (string, error) {
				calls = append(calls, step.ID)
				if step.ID == "fetch" && fail {
					return `{"partial":true}`, &workflow.ExecutionError{Code: "http_status", Err: errors.New("HTTP status 503")}
				}
				if step.ID == "report" && string(step.Args) != `{"code":"http_status"}` {
					t.Fatal("failure data not resolved", string(step.Args))
				}
				return `{}`, nil
			}}
			r, err := Run(t.Context(), d, h, func(workflow.Run, ...int) error { return nil })
			want := []string{"fetch", "after", "finish"}
			if fail {
				want = []string{"fetch", "report", "finish"}
			}
			if !slices.Equal(calls, want) || (err != nil) != fail || r.Finished == nil {
				t.Fatal(calls, r, err)
			}
			if fail {
				var execution *workflow.ExecutionError
				if !errors.As(err, &execution) || execution.Code != "http_status" || r.Status != "failed" || r.Steps[0].Failure == nil || r.Steps[0].Output != `{"partial":true}` {
					t.Fatal(r, err)
				}
			} else if r.Status != "completed" {
				t.Fatal(r)
			}
		})
	}
}

func TestRuntimeAndCheckpointErrorsNeverRunErrorRoutesOrFinalizers(t *testing.T) {
	for _, mode := range []string{"permission", "cancel", "checkpoint", "joined checkpoint"} {
		t.Run(mode, func(t *testing.T) {
			d := workflow.Definition{Version: 4, Name: "Stop", Steps: []workflow.Step{
				{ID: "effect", Tool: "probe", Args: json.RawMessage(`{}`)},
				{ID: "report", Tool: "probe", OnFailure: "effect", Args: json.RawMessage(`{}`)},
				{ID: "finish", Tool: "probe", Finally: true, Args: json.RawMessage(`{}`)},
			}}
			if mode == "joined checkpoint" {
				d.Steps[0].ForEach = json.RawMessage(`[{}]`)
			}
			calls := 0
			h := collectionHandler{execute: func(_ context.Context, step workflow.Step) (string, error) {
				calls++
				if step.ID != "effect" && step.ID != "effect[0]" {
					t.Error("routed a non-operation failure", step.ID)
				}
				if mode == "permission" {
					return "", errors.New("permission denied")
				}
				if mode == "cancel" {
					return "", &workflow.ExecutionError{Err: context.Canceled}
				}
				return "", &workflow.ExecutionError{Err: errors.New("service unavailable")}
			}}
			save := func(r workflow.Run, _ ...int) error {
				if mode == "joined checkpoint" && len(r.Steps[0].Iterations) > 0 {
					item := r.Steps[0].Iterations[0]
					if len(item.Attempts) > 0 && item.Attempts[0].Finished != nil {
						return errors.New("disk full")
					}
				}
				if mode == "checkpoint" && len(r.Steps[0].Attempts) > 0 && r.Steps[0].Attempts[0].Finished != nil {
					return errors.New("disk full")
				}
				return nil
			}
			_, err := Run(t.Context(), d, h, save)
			if err == nil || calls != 1 {
				t.Fatal(calls, err)
			}
		})
	}
}

func TestFinalizerFailureDoesNotHidePrimaryFailureOrStopOtherFinalizers(t *testing.T) {
	d := workflow.Definition{Version: 4, Name: "Finish", Steps: []workflow.Step{
		{ID: "fetch", Tool: "probe", Args: json.RawMessage(`{}`)},
		{ID: "cleanup", Tool: "probe", Finally: true, Args: json.RawMessage(`{}`)},
		{ID: "report", Tool: "probe", Finally: true, Args: json.RawMessage(`{}`)},
	}}
	var calls []string
	h := collectionHandler{execute: func(_ context.Context, step workflow.Step) (string, error) {
		calls = append(calls, step.ID)
		if step.ID != "report" {
			return "", &workflow.ExecutionError{Code: step.ID, Err: errors.New(step.ID + " failed")}
		}
		return `{}`, nil
	}}
	r, err := Run(t.Context(), d, h, func(workflow.Run, ...int) error { return nil })
	var execution *workflow.ExecutionError
	if !errors.As(err, &execution) || execution.Code != "fetch" || !slices.Equal(calls, []string{"fetch", "cleanup", "report"}) || r.Steps[1].Failure.Code != "cleanup" {
		t.Fatal(r, calls, err)
	}
}

func TestRecoveryRestoresTypedCollectionFailureBeforeParentCheckpoint(t *testing.T) {
	d := workflow.Definition{Version: 4, Name: "Failure checkpoint", Steps: []workflow.Step{
		{ID: "fetch", Tool: "probe", Args: json.RawMessage(`{}`), ForEach: json.RawMessage(`[{},{}]`)},
		{ID: "notify", Tool: "probe", OnFailure: "fetch", Args: json.RawMessage(`{}`)},
		{ID: "finish", Tool: "probe", Finally: true, Args: json.RawMessage(`{}`)},
	}}
	h := collectionHandler{execute: func(_ context.Context, step workflow.Step) (string, error) {
		if step.ID != "fetch[0]" {
			t.Error("effects past interrupted checkpoint", step.ID)
		}
		return "", &workflow.ExecutionError{Code: "unavailable", Err: errors.New("source unavailable")}
	}}
	var checkpoint workflow.Run
	stopped := false
	save := func(r workflow.Run, _ ...int) error {
		if stopped {
			return errors.New("checkpoint unavailable")
		}
		if len(r.Steps[0].Iterations) > 0 && r.Steps[0].Iterations[0].Failure != nil {
			raw, err := json.Marshal(r)
			if err != nil {
				return err
			}
			err = json.Unmarshal(raw, &checkpoint)
			stopped = true
			return err
		}
		return nil
	}
	_, err := Run(t.Context(), d, h, save)
	if err == nil || !stopped || checkpoint.Steps[0].Status != "running" {
		t.Fatal(checkpoint, err)
	}
	var calls []string
	h = collectionHandler{execute: func(_ context.Context, step workflow.Step) (string, error) {
		calls = append(calls, step.ID)
		return `{}`, nil
	}}
	r, err := Recover(t.Context(), checkpoint, workflow.Recovery{Note: "Leaf failure committed before parent outcome"}, h, func(workflow.Run, ...int) error { return nil })
	var execution *workflow.ExecutionError
	if !errors.As(err, &execution) || execution.Code != "unavailable" || r.Status != "failed" || !slices.Equal(calls, []string{"notify", "finish"}) {
		t.Fatal(r, calls, err)
	}
}
