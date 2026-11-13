// Package workflow describes runtime recipe-call input binding and output collection.
package workflow

import (
	"context"
	"encoding/json"

	"github.com/keshon/gektor/internal/workflow"
)

func Definitions() []workflow.Node {
	return []workflow.Node{
		{
			Name:        "workflow_call",
			Version:     1,
			Description: "Invoke a frozen workspace recipe with explicit typed inputs. Declared child outputs become this step's output. For mapped calls, args contains only literal recipe_path and for_each contains child input objects; calls execute serially and retain ordered item outcomes. Child scopes share permissions, reviews, artifacts and invocation budget. Calls cannot retry.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"recipe_path":{"type":"string","minLength":1},"inputs":{"type":"object","maxProperties":32}},"required":["recipe_path"],"additionalProperties":false}`),
			Resources: func(raw json.RawMessage) ([]string, error) {
				var in workflow.CallInput
				err := json.Unmarshal(raw, &in)
				return []string{in.Path}, err
			},
			// The engine owns child execution; this authorized operation supplies its inputs.
			Execute: func(_ context.Context, raw json.RawMessage) (string, error) {
				var in workflow.CallInput
				err := json.Unmarshal(raw, &in)
				if err != nil {
					return "", err
				}
				if in.Inputs == nil {
					in.Inputs = map[string]json.RawMessage{}
				}
				bound, err := json.Marshal(in.Inputs)
				return string(bound), err
			},
		},
		{
			Name: "workflow_value", Internal: true, Version: 1,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Resources:   func(json.RawMessage) ([]string, error) { return nil, nil },
			Execute:     func(_ context.Context, raw json.RawMessage) (string, error) { return string(raw), nil },
		},
	}
}
