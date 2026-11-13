package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func repeatDefinition(limit int) workflow.Definition {
	child := workflow.Definition{Version: 4, Name: "page", Inputs: map[string]workflow.Input{"cursor": {Type: "integer", Required: true}}, Steps: []workflow.Step{
		{ID: "fetch", Tool: "probe", Args: json.RawMessage(`{"cursor":{"$input":"cursor"}}`)},
		{ID: "@outputs", Tool: "workflow_value", CollectOutputs: true, Args: json.RawMessage(`{"$step":"fetch"}`)},
	}}
	return workflow.Definition{Version: 4, Name: "repeat", Steps: []workflow.Step{{
		ID: "pages", Tool: "workflow_call", Child: &child,
		Args:   json.RawMessage(`{"recipe_path":"page.json","inputs":{"cursor":0}}`),
		Repeat: &workflow.RepeatPolicy{MaxIterations: limit, Until: "/done", NextInputs: "/next"},
	}}}
}

func TestRepeatAdvancesInputsAndRetainsOrderedScopes(t *testing.T) {
	for _, count := range []int{1, 3, 500} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			calls := 0
			h := collectionHandler{execute: func(_ context.Context, s workflow.Step) (string, error) {
				if s.Tool == "workflow_call" {
					return callInputs(s)
				}
				if s.Tool == "workflow_value" {
					return string(s.Args), nil
				}
				want := fmt.Sprintf(`{"cursor":%d}`, calls)
				if string(s.Args) != want || s.ID != fmt.Sprintf("pages[%d]/fetch", calls) {
					t.Fatalf("identity/inputs: id=%s args=%s want=%s", s.ID, s.Args, want)
				}
				calls++
				return fmt.Sprintf(`{"done":%t,"next":{"cursor":%d}}`, calls == count, calls), nil
			}}
			r, err := Run(t.Context(), repeatDefinition(count), h, func(workflow.Run, ...int) error { return nil })
			if err != nil || r.Status != "completed" || calls != count || len(r.Steps[0].Iterations) != count {
				t.Fatalf("calls=%d status=%s err=%v", calls, r.Status, err)
			}
			if r.Steps[0].Output != fmt.Sprintf(`{"done":true,"next":{"cursor":%d}}`, count) {
				t.Fatal("repeat did not return the final child output", r.Steps[0].Output)
			}
		})
	}
}

func TestRepeatStopsAtInvalidControlAndExecutionFailures(t *testing.T) {
	for _, mode := range []string{"limit", "missing until", "null until", "string until", "missing next", "null next", "array next", "execution", "checkpoint", "checkpoint after effect"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			h := collectionHandler{execute: func(_ context.Context, s workflow.Step) (string, error) {
				if s.Tool == "workflow_call" {
					return callInputs(s)
				}
				if s.Tool == "workflow_value" {
					return string(s.Args), nil
				}
				calls++
				switch mode {
				case "missing until":
					return `{}`, nil
				case "null until":
					return `{"done":null}`, nil
				case "string until":
					return `{"done":"true"}`, nil
				case "missing next":
					return `{"done":false}`, nil
				case "null next":
					return `{"done":false,"next":null}`, nil
				case "array next":
					return `{"done":false,"next":[]}`, nil
				case "execution":
					return "", &workflow.ExecutionError{Err: errors.New("source unavailable")}
				}
				return `{"done":false,"next":{"cursor":1}}`, nil
			}}
			r, err := Run(t.Context(), repeatDefinition(3), h, func(record workflow.Run, path ...int) error {
				if mode == "checkpoint after effect" && len(path) == 2 && record.Steps[0].Iterations[path[1]].Status == "completed" {
					return errors.New("outcome checkpoint unavailable")
				}
				if mode == "checkpoint" && len(path) == 2 {
					return errors.New("storage unavailable")
				}
				return nil
			})
			want := 1
			if mode == "limit" {
				want = 3
			}
			if mode == "checkpoint" {
				want = 0
			}
			if err == nil || r.Status != "failed" || calls != want {
				t.Fatalf("calls=%d want=%d status=%s err=%v", calls, want, r.Status, err)
			}
			if mode == "limit" && !strings.Contains(err.Error(), "max_iterations") {
				t.Fatal(err)
			}
		})
	}
}

func TestRepeatReviewContinuationDoesNotReplayEarlierItems(t *testing.T) {
	d := repeatDefinition(3)
	d.Steps[0].Child.Steps[0].Review = true
	calls := 0
	h := collectionHandler{execute: func(_ context.Context, s workflow.Step) (string, error) {
		if s.Tool == "workflow_call" {
			return callInputs(s)
		}
		if s.Tool == "workflow_value" {
			return string(s.Args), nil
		}
		calls++
		return fmt.Sprintf(`{"done":%t,"next":{"cursor":%d}}`, calls == 3, calls), nil
	}}
	save := func(workflow.Run, ...int) error { return nil }
	r, err := Run(t.Context(), d, h, save)
	for n := 0; n < 3; n++ {
		if err != nil || r.Status != "waiting_review" || calls != n {
			t.Fatalf("item=%d calls=%d status=%s err=%v", n, calls, r.Status, err)
		}
		id := fmt.Sprintf("pages[%d]/fetch", n)
		found := false
		for _, step := range workflow.ScopedSteps(r) {
			if step.ID == id && step.Result.Status == "waiting_review" && step.Definition.Review {
				found = true
			}
		}
		if !found {
			t.Fatal("review identity missing", id)
		}
		raw, encodeErr := json.Marshal(r)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		err = json.Unmarshal(raw, &r)
		if err != nil {
			t.Fatal(err)
		}
		if r.Approved == nil {
			r.Approved = map[string]bool{}
		}
		r.Approved[id] = true
		r, err = Resume(t.Context(), r, h, save)
	}
	if err != nil || r.Status != "completed" || calls != 3 {
		t.Fatalf("calls=%d status=%s err=%v", calls, r.Status, err)
	}
}

func TestRepeatSharesBudgetAndCancelsDelay(t *testing.T) {
	for _, mode := range []string{"budget", "delay"} {
		t.Run(mode, func(t *testing.T) {
			d := repeatDefinition(3)
			calls := 0
			h := collectionHandler{execute: func(_ context.Context, s workflow.Step) (string, error) {
				if s.Tool == "workflow_call" {
					return callInputs(s)
				}
				if s.Tool == "workflow_value" {
					return string(s.Args), nil
				}
				calls++
				return `{"done":false,"next":{"cursor":1}}`, nil
			}}
			ctx := t.Context()
			if mode == "delay" {
				d.Steps[0].Repeat.DelayMS = 10000
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			remaining := 4 // Parent, one item, and two child steps.
			if mode == "delay" {
				remaining = 100
			}
			r, err := run(ctx, d, nil, h, func(workflow.Run, ...int) error { return nil }, nil, executionScope{remaining: &remaining})
			if err == nil || calls != 1 || invocationCount(r) != 4 {
				t.Fatalf("calls=%d count=%d err=%v", calls, invocationCount(r), err)
			}
			if mode == "delay" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("delay did not preserve cancellation", err)
			}
		})
	}
}

func TestRepeatNestedScopesKeepQualifiedReviewIdentity(t *testing.T) {
	d := repeatDefinition(3)
	child := repeatDefinition(2)
	child.Inputs = d.Steps[0].Child.Inputs
	child.Steps[0].Child.Steps[0].Review = true
	child.Steps = append(child.Steps, workflow.Step{ID: "@outputs", Tool: "workflow_value", CollectOutputs: true, Args: json.RawMessage(`{"$step":"pages"}`)})
	d.Steps[0].Child = &child
	calls := 0
	h := collectionHandler{execute: func(_ context.Context, s workflow.Step) (string, error) {
		if s.Tool == "workflow_call" {
			return callInputs(s)
		}
		if s.Tool == "workflow_value" {
			return string(s.Args), nil
		}
		calls++
		return fmt.Sprintf(`{"done":%t,"next":{"cursor":%d}}`, calls == 2, calls), nil
	}}
	save := func(workflow.Run, ...int) error { return nil }
	r, err := Run(t.Context(), d, h, save)
	for n := 0; n < 2; n++ {
		if err != nil || r.Status != "waiting_review" || calls != n {
			t.Fatalf("calls=%d status=%s err=%v", calls, r.Status, err)
		}
		id := fmt.Sprintf("pages[0]/pages[%d]/fetch", n)
		found := false
		for _, step := range workflow.ScopedSteps(r) {
			if step.ID == id && step.Result.Status == "waiting_review" && step.Definition.Review {
				found = true
			}
		}
		if !found {
			t.Fatal("nested review identity missing", id)
		}
		if r.Approved == nil {
			r.Approved = map[string]bool{}
		}
		r.Approved[id] = true
		r, err = Resume(t.Context(), r, h, save)
	}
	if err != nil || r.Status != "completed" || calls != 2 {
		t.Fatalf("calls=%d status=%s err=%v", calls, r.Status, err)
	}
}
