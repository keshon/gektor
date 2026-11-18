package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
)

func TestRetryCheckpointsAndCancellation(t *testing.T) {
	for _, mode := range []string{"success", "checkpoint", "cancel after outcome"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls, saves := 0, 0
			h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) {
				calls++
				if calls == 1 {
					return "partial", &workflow.ExecutionError{Code: "temporary", Err: errors.New("busy")}
				}
				return `{}`, nil
			}}
			s := workflow.Step{ID: "fetch", Tool: "probe", Args: json.RawMessage(`{}`), Retry: &workflow.RetryPolicy{MaxAttempts: 3, Codes: []string{"temporary"}}}
			r := workflow.Run{Steps: []workflow.StepResult{{ID: "fetch"}}}
			diskErr := errors.New("disk full")
			save := func(record workflow.Run, _ ...int) error {
				saves++
				if saves == 1 && calls != 0 {
					t.Fatal("effect preceded attempt checkpoint")
				}
				if saves == 2 {
					attempt := record.Steps[0].Attempts[0]
					if calls != 1 || attempt.Failure == nil || attempt.Failure.Code != "temporary" {
						t.Fatalf("outcome not checkpointed: %+v", attempt)
					}
					if mode == "checkpoint" {
						return diskErr
					}
					if mode == "cancel after outcome" {
						cancel()
					}
				}
				return nil
			}

			out, err := executeAttempts(ctx, s, h, &r, 0, save)
			switch mode {
			case "success":
				if err != nil || calls != 2 || out != `{}` || len(r.Steps[0].Attempts) != 2 || r.Steps[0].Attempts[1].Failure != nil {
					t.Fatal(out, err, r)
				}
			case "checkpoint":
				if !errors.Is(err, diskErr) || calls != 1 {
					t.Fatal(err, calls)
				}
			case "cancel after outcome":
				if !errors.Is(err, context.Canceled) || calls != 1 {
					t.Fatal(err, calls)
				}
			}
		})
	}
}

func TestErrorRoutesWaitForRetryResult(t *testing.T) {
	for _, succeeds := range []bool{false, true} {
		name := "exhausted"
		if succeeds {
			name = "succeeds"
		}
		t.Run(name, func(t *testing.T) {
			calls, routes, finalizers := 0, 0, 0
			h := collectionHandler{execute: func(_ context.Context, step workflow.Step) (string, error) {
				switch step.ID {
				case "fetch":
					calls++
					if !succeeds || calls == 1 {
						return "", &workflow.ExecutionError{Code: "temporary", Err: errors.New("busy")}
					}
				case "report":
					routes++
					if calls != 2 {
						t.Fatal("route ran before attempts settled")
					}
				case "finish":
					finalizers++
					if calls != 2 {
						t.Fatal("finalizer ran before attempts settled")
					}
				}
				return `{}`, nil
			}}
			d := workflow.Definition{Version: 4, Name: "retry routes", Steps: []workflow.Step{
				{ID: "fetch", Tool: "probe", Args: json.RawMessage(`{}`), Retry: &workflow.RetryPolicy{MaxAttempts: 2, Codes: []string{"temporary"}}},
				{ID: "report", Tool: "probe", Args: json.RawMessage(`{}`), OnFailure: "fetch"},
				{ID: "finish", Tool: "probe", Args: json.RawMessage(`{}`), Finally: true},
			}}

			record, err := Run(t.Context(), d, h, func(workflow.Run, ...int) error { return nil })
			wantRoutes := 1
			if succeeds {
				wantRoutes = 0
			}
			if (err == nil) != succeeds || calls != 2 || routes != wantRoutes || finalizers != 1 {
				t.Fatal(record, err, calls, routes, finalizers)
			}
		})
	}
}
