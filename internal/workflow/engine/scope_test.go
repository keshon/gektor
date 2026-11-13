package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func childDefinition() workflow.Definition {
	return workflow.Definition{Version: 4, Name: "child", Steps: []workflow.Step{
		{ID: "effect", Args: json.RawMessage(`{}`)},
		{ID: "@outputs", Tool: "workflow_value", CollectOutputs: true, Args: json.RawMessage(`{"done":true}`)},
	}}
}

func TestChildCheckpointFailurePreventsEffects(t *testing.T) {
	child := childDefinition()
	d := workflow.Definition{Version: 4, Name: "parent", Steps: []workflow.Step{{ID: "call", Tool: "workflow_call", Args: json.RawMessage(`{}`), Child: &child}}}
	effects := 0
	h := collectionHandler{execute: func(_ context.Context, step workflow.Step) (string, error) {
		if step.Tool == "workflow_call" {
			return `{}`, nil
		}
		effects++
		return `{}`, nil
	}}
	r, err := Run(t.Context(), d, h, func(r workflow.Run, path ...int) error {
		if len(path) > 1 && path[1] == workflow.ChildScope {
			return errors.New("child checkpoint fault")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "child checkpoint fault") || effects != 0 || r.Status != "failed" {
		t.Fatalf("effect without child checkpoint: status=%s effects=%d err=%v", r.Status, effects, err)
	}
}

func TestChildLeafWorkersJoinOnCancellation(t *testing.T) {
	child := collectionDefinition(t, 10, 3)
	child.Steps[0].OnError = "stop"
	child.Steps = append(child.Steps, workflow.Step{ID: "@outputs", Tool: "workflow_value", CollectOutputs: true, Args: json.RawMessage(`{}`)})
	d := workflow.Definition{Version: 4, Name: "parent", Steps: []workflow.Step{{ID: "call", Tool: "workflow_call", Args: json.RawMessage(`{}`), Child: &child}}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var active, started atomic.Int32
	h := collectionHandler{execute: func(ctx context.Context, step workflow.Step) (string, error) {
		if step.Tool == "workflow_call" {
			return `{}`, nil
		}
		active.Add(1)
		defer active.Add(-1)
		if started.Add(1) == 3 {
			cancel()
		}
		<-ctx.Done()
		return "", ctx.Err()
	}}
	r, err := Run(ctx, d, h, func(workflow.Run, ...int) error { return nil })
	if !errors.Is(err, context.Canceled) || active.Load() != 0 || started.Load() != 3 || r.Status != "cancelled" || r.Steps[0].Child.Status != "cancelled" {
		t.Fatalf("child workers escaped cancellation: active=%d started=%d status=%s err=%v", active.Load(), started.Load(), r.Status, err)
	}
}

func TestChildScopesShareInvocationBudget(t *testing.T) {
	child := collectionDefinition(t, 3, 2)
	child.Steps = append(child.Steps, workflow.Step{ID: "@outputs", Tool: "workflow_value", CollectOutputs: true, Args: json.RawMessage(`{}`)})
	d := workflow.Definition{Version: 4, Name: "parent", Steps: []workflow.Step{{ID: "call", Tool: "workflow_call", Args: json.RawMessage(`{}`), Child: &child}}}
	effects := 0
	h := collectionHandler{execute: func(_ context.Context, step workflow.Step) (string, error) {
		if step.Tool == "workflow_call" {
			return `{}`, nil
		}
		effects++
		return `{}`, nil
	}}
	remaining := 4 // Parent and child steps leave only one slot for the three items.
	r, err := run(t.Context(), d, nil, h, func(workflow.Run, ...int) error { return nil }, nil, executionScope{remaining: &remaining})
	if err == nil || !strings.Contains(err.Error(), "invocation slots") || effects != 0 || remaining != 1 || r.Status != "failed" {
		t.Fatalf("child received a fresh budget: remaining=%d effects=%d err=%v", remaining, effects, err)
	}
}
