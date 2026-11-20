package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestSelectiveRetries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		codes   []string
		failure error
		safe    bool
		calls   int
	}{
		{name: "matching", codes: []string{"temporary"}, failure: &workflow.ExecutionError{Code: "temporary", Err: errors.New("busy")}, safe: true, calls: 2},
		{name: "nonmatching", codes: []string{"temporary"}, failure: &workflow.ExecutionError{Code: "permanent", Err: errors.New("bad request")}, safe: true, calls: 1},
		{name: "omitted", failure: &workflow.ExecutionError{Code: "temporary", Err: errors.New("busy")}, safe: true, calls: 2},
		{name: "default code", codes: []string{workflow.OperationFailed}, failure: &workflow.ExecutionError{Err: errors.New("busy")}, safe: true, calls: 2},
		{name: "untyped", codes: []string{"temporary"}, failure: errors.New("permission denied"), safe: true, calls: 1},
		{name: "cancelled", failure: &workflow.ExecutionError{Code: "temporary", Err: context.Canceled}, safe: true, calls: 1},
		{name: "deadline", failure: &workflow.ExecutionError{Code: "temporary", Err: context.DeadlineExceeded}, safe: true, calls: 1},
		{name: "unsafe", codes: []string{"temporary"}, safe: false, calls: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()

			calls := 0
			node := workflow.Node{
				Name: "probe", Version: 1, RetrySafe: tc.safe,
				Resources:    func(json.RawMessage) ([]string, error) { return nil, nil },
				InputSchema:  json.RawMessage(`{"type":"object"}`),
				OutputSchema: json.RawMessage(`{"type":"object"}`),
				Execute: func(context.Context, json.RawMessage) (string, error) {
					calls++
					return "{}", tc.failure
				},
			}
			registry, err := workflow.NewRegistry(node)
			if err != nil {
				t.Fatal(err)
			}

			definition := workflow.Definition{Version: 4, Name: "retry", Steps: []workflow.Step{{
				ID: "probe", Tool: "probe", Args: json.RawMessage(`{}`),
				Retry: &workflow.RetryPolicy{MaxAttempts: 2, Codes: tc.codes},
			}}}
			source, err := json.Marshal(definition)
			if err != nil {
				t.Fatal(err)
			}
			err = ws.WriteFile("recipe.json", source, 0600)
			if err != nil {
				t.Fatal(err)
			}

			svc := Service{WS: ws, StoreDir: t.TempDir(), Registry: registry,
				Authorize: func(context.Context, string, json.RawMessage) error { return nil },
			}
			out, err := svc.Run(t.Context(), "recipe.json", nil)
			if err == nil || calls != tc.calls {
				t.Fatalf("calls=%d, want %d; result=%+v err=%v", calls, tc.calls, out, err)
			}
			if !tc.safe {
				return
			}

			summary := out
			db, err := store.OpenStore(svc.StoreDir, ws.Root())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			state, err := db.Run(summary.RunID)
			if err != nil {
				t.Fatal(err)
			}
			attempts := state.Record.Steps[0].Attempts
			if len(attempts) != tc.calls {
				t.Fatalf("attempts=%+v", attempts)
			}
			want := workflow.OperationFailure(tc.failure)
			for _, attempt := range attempts {
				if attempt.Finished == nil || (attempt.Failure == nil) != (want == nil) {
					t.Fatalf("attempt=%+v", attempt)
				}
				if want != nil && attempt.Failure.Code != want.Code {
					t.Fatalf("attempt=%+v want=%+v", attempt, want)
				}
			}
		})
	}
}

func TestRetryCodeValidation(t *testing.T) {
	registry, err := workflow.NewRegistry(workflow.Node{
		Name: "probe", Version: 1, RetrySafe: true,
		Resources:   func(json.RawMessage) ([]string, error) { return nil, nil },
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Execute:     func(context.Context, json.RawMessage) (string, error) { return "{}", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	h := handler{registry: registry}

	for _, codes := range [][]string{{""}, {" temporary"}, {"temporary "}, {"temporary", "temporary"}, {strings.Repeat("x", 65)}, make([]string, 33)} {
		step := workflow.Step{ID: "probe", Tool: "probe", Args: json.RawMessage(`{}`), Retry: &workflow.RetryPolicy{MaxAttempts: 2, Codes: codes}}
		err = h.Validate(step)
		if err == nil {
			t.Fatalf("accepted invalid codes: %q", codes)
		}
	}
}
