package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
)

func mappedDefinition() workflow.Definition {
	child := workflow.Definition{Version: 4, Name: "child", Inputs: map[string]workflow.Input{"value": {Type: "integer", Required: true}}, Steps: []workflow.Step{
		{ID: "effect", Tool: "probe", Args: json.RawMessage(`{"value":{"$input":"value"}}`)},
		{ID: "@outputs", Tool: "workflow_value", CollectOutputs: true, Args: json.RawMessage(`{"value":{"$step":"effect","pointer":"/value"}}`)},
	}}
	return workflow.Definition{Version: 4, Name: "map", Steps: []workflow.Step{{ID: "batch", Tool: "workflow_call", Child: &child, Args: json.RawMessage(`{"recipe_path":"child.json"}`), ForEach: json.RawMessage(`[{"value":0},{"value":1},{"value":2}]`), OnError: "collect"}}}
}

func callInputs(s workflow.Step) (string, error) {
	var in workflow.CallInput
	err := json.Unmarshal(s.Args, &in)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(in.Inputs)
	return string(raw), err
}

func TestMappedCallsCollectOnlyExecutionFailures(t *testing.T) {
	for _, mode := range []string{"collect", "permission", "invalid output", "checkpoint"} {
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
				if string(s.Args) != `{"value":1}` {
					return string(s.Args), nil
				}
				if mode == "collect" {
					return "", &workflow.ExecutionError{Err: errors.New("unavailable")}
				}
				if mode == "invalid output" {
					return "not JSON", nil
				}
				return "", errors.New("permission denied")
			}}
			r, err := Run(t.Context(), mappedDefinition(), h, func(_ workflow.Run, path ...int) error {
				if mode == "checkpoint" && len(path) >= 4 && path[0] == 0 && path[1] == 1 && path[2] == workflow.ChildScope {
					return errors.New("storage fault")
				}
				return nil
			})
			items := r.Steps[0].Iterations
			if mode == "collect" {
				if err != nil || r.Status != "completed" || calls != 3 || items[1].Status != "failed" || items[1].Child.Status != "failed" || items[2].Output != `{"value":2}` {
					t.Fatalf("lost collection outcomes: status=%s calls=%d err=%v", r.Status, calls, err)
				}
				return
			}
			wantCalls := 2
			if mode == "checkpoint" {
				wantCalls = 1
			}
			if err == nil || r.Status != "failed" || calls != wantCalls || items[0].Status != "completed" || items[2].Status != "skipped" {
				t.Fatalf("collected a control failure: status=%s calls=%d err=%v", r.Status, calls, err)
			}
		})
	}
}

func TestMappedCallsPreserveEarlierEffectsAtBudgetLimit(t *testing.T) {
	for _, capacity := range []int{3, 6} {
		calls := 0
		h := collectionHandler{execute: func(_ context.Context, s workflow.Step) (string, error) {
			if s.Tool == "workflow_call" {
				return callInputs(s)
			}
			if s.Tool == "probe" {
				calls++
			}
			return string(s.Args), nil
		}}
		remaining := capacity
		r, err := run(t.Context(), mappedDefinition(), nil, h, func(workflow.Run, ...int) error { return nil }, nil, executionScope{remaining: &remaining})
		if err == nil || !strings.Contains(err.Error(), "invocation") || r.Status != "failed" || invocationCount(r) > capacity {
			t.Fatalf("budget exceeded: capacity=%d count=%d err=%v", capacity, invocationCount(r), err)
		}
		if capacity == 3 {
			if calls != 0 || len(r.Steps[0].Iterations) != 0 {
				t.Fatal("array admission had effects", calls)
			}
			continue
		}
		items := r.Steps[0].Iterations
		if calls != 1 || remaining != 0 || items[0].Status != "completed" || items[1].Status != "failed" || items[2].Status != "skipped" {
			t.Fatal("budget failure lost earlier work", calls, remaining, items)
		}
	}
}

func TestMappedCallsCancelBeforeDispatchingNextItem(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	h := collectionHandler{execute: func(_ context.Context, s workflow.Step) (string, error) {
		if s.Tool == "workflow_call" {
			return callInputs(s)
		}
		if s.Tool == "probe" {
			calls++
		}
		return string(s.Args), nil
	}}
	r, err := Run(ctx, mappedDefinition(), h, func(r workflow.Run, path ...int) error {
		if len(path) == 2 && path[1] == 0 && r.Steps[0].Iterations[0].Status == "completed" {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || r.Status != "cancelled" || calls != 1 || r.Steps[0].Iterations[1].Status != "skipped" || r.Steps[0].Iterations[2].Status != "skipped" {
		t.Fatal("dispatched after cancellation", r.Status, calls, err)
	}
}

func TestEmptyMappedCallDoesNotInstantiateChildren(t *testing.T) {
	d := mappedDefinition()
	d.Steps[0].ForEach = json.RawMessage(`[]`)
	h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) {
		t.Error("empty map executed a call")
		return "", nil
	}}
	r, err := Run(t.Context(), d, h, func(workflow.Run, ...int) error { return nil })
	if err != nil || r.Status != "completed" || r.Steps[0].Output != `{"items":[]}` || invocationCount(r) != 1 {
		t.Fatal(r, err)
	}
}

func TestMappedCallsAccountForFiveHundredScopes(t *testing.T) {
	d := mappedDefinition()
	inputs := make([]map[string]int, 500)
	for n := range inputs {
		inputs[n] = map[string]int{"value": n}
	}
	raw, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	d.Steps[0].ForEach = raw
	calls := 0
	h := collectionHandler{execute: func(_ context.Context, s workflow.Step) (string, error) {
		if s.Tool == "workflow_call" {
			return callInputs(s)
		}
		if s.Tool == "workflow_value" {
			return string(s.Args), nil
		}
		calls++
		var in struct{ Value int }
		decodeErr := json.Unmarshal(s.Args, &in)
		if decodeErr != nil {
			return "", decodeErr
		}
		if in.Value%7 == 0 {
			return "", &workflow.ExecutionError{Err: errors.New("unavailable")}
		}
		return string(s.Args), nil
	}}
	r, err := Run(t.Context(), d, h, func(workflow.Run, ...int) error { return nil })
	if err != nil || r.Status != "completed" || calls != 500 || invocationCount(r) != 1501 {
		t.Fatal("incomplete scope accounting", r.Status, calls, invocationCount(r), err)
	}
	var out struct{ Items []iterationOutcome }
	err = json.Unmarshal([]byte(r.Steps[0].Output), &out)
	if err != nil || len(out.Items) != 500 {
		t.Fatal("lost outcomes", len(out.Items), err)
	}
	for n, outcome := range out.Items {
		status := "completed"
		if n%7 == 0 {
			status = "failed"
		}
		if outcome.Index != n || outcome.Status != status || r.Steps[0].Iterations[n].Child.Status != status {
			t.Fatal("scope outcome mismatch", n, outcome)
		}
		if status == "completed" && string(outcome.Output) != fmt.Sprintf(`{"value":%d}`, n) {
			t.Fatal("scope output mismatch", n, outcome)
		}
	}
}

func TestMappedCallsBoundAggregateOutputBeforeMoreEffects(t *testing.T) {
	d := mappedDefinition()
	d.Steps[0].ForEach = json.RawMessage("[" + strings.TrimSuffix(strings.Repeat(`{"value":0},`, 24), ",") + "]")
	output := `{"value":"` + strings.Repeat("x", 55000) + `"}`
	calls := 0
	h := collectionHandler{execute: func(_ context.Context, s workflow.Step) (string, error) {
		if s.Tool == "workflow_call" {
			return callInputs(s)
		}
		if s.Tool == "workflow_value" {
			return string(s.Args), nil
		}
		calls++
		return output, nil
	}}
	r, err := Run(t.Context(), d, h, func(workflow.Run, ...int) error { return nil })
	items := r.Steps[0].Iterations
	if err == nil || !strings.Contains(err.Error(), "mapped child output exceeds") || r.Status != "failed" || calls == 0 || calls >= 24 || items[0].Status != "completed" || items[23].Status != "skipped" {
		t.Fatal("unbounded mapped output", r.Status, calls, err)
	}
}
