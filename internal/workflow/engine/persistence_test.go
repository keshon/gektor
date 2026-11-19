package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
)

func TestTransientCheckpointFailurePreservesUncertainEffectsAcrossScopes(t *testing.T) {
	for _, mode := range []string{"root", "collection", "child", "mapped child", "repeated child"} {
		t.Run(mode, func(t *testing.T) {
			d := workflow.Definition{Version: 4, Name: mode, Steps: []workflow.Step{{ID: "effect", Tool: "probe", Args: json.RawMessage(`{}`)}}}
			if mode == "collection" {
				d.Steps[0].ForEach = json.RawMessage(`[{},{}]`)
			}
			if mode == "child" || mode == "mapped child" || mode == "repeated child" {
				child := childDefinition()
				d.Steps = []workflow.Step{{ID: "call", Tool: "workflow_call", Args: json.RawMessage(`{}`), Child: &child}}
			}
			if mode == "mapped child" {
				d.Steps[0].ForEach = json.RawMessage(`[{},{}]`)
			}
			if mode == "repeated child" {
				d.Steps[0].Repeat = &workflow.RepeatPolicy{MaxIterations: 2, Until: "/done", NextInputs: "/next"}
			}
			effects := 0
			h := collectionHandler{execute: func(_ context.Context, step workflow.Step) (string, error) {
				if step.Tool == "workflow_call" {
					return `{}`, nil
				}
				if step.Tool == "workflow_value" {
					return string(step.Args), nil
				}
				effects++
				return `{}`, nil
			}}
			cause := errors.New("one lost checkpoint")
			failed := false
			writesAfterFailure := 0
			var saved workflow.Run
			run, err := Run(t.Context(), d, h, func(record workflow.Run, _ ...int) error {
				if failed {
					writesAfterFailure++
				}
				if effects > 0 && !failed {
					failed = true
					return cause
				}
				raw, encodeErr := json.Marshal(record)
				if encodeErr != nil {
					return encodeErr
				}
				return json.Unmarshal(raw, &saved)
			})
			if !errors.Is(err, cause) || effects != 1 || run.Status != "running" || writesAfterFailure != 0 {
				t.Fatal("checkpoint fault was overwritten or execution continued", run.Status, effects, writesAfterFailure, err)
			}
			uncertain := workflow.UncertainSteps(saved)
			if saved.Status != "running" || len(uncertain) != 1 || uncertain[0].Result.Status != "running" {
				t.Fatal("lost the durable uncertain invocation", saved, uncertain)
			}
		})
	}
}
