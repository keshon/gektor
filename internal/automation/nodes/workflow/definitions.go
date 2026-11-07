// Package workflow describes recipe-call nodes expanded before workflow execution.
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/recipe"
)

type Binding struct {
	Path   string                      `json:"recipe_path"`
	Specs  map[string]automation.Input `json:"specs"`
	Values map[string]json.RawMessage  `json:"values"`
}

func Definitions() []automation.Node {
	validateBinding := func(raw json.RawMessage) error {
		var in Binding
		if err := json.Unmarshal(raw, &in); err != nil {
			return err
		}
		_, err := recipe.BindInputs(in.Specs, in.Values)
		return err
	}
	resources := func(raw json.RawMessage) ([]string, error) {
		var in struct {
			Path string `json:"recipe_path"`
		}
		err := json.Unmarshal(raw, &in)
		return []string{in.Path}, err
	}
	return []automation.Node{
		{Name: "workflow_call", Version: 1, Description: "Invoke a saved workspace recipe with explicit typed inputs. Its declared outputs become this step's output. Child steps share this run, permissions, review and artifacts. Path must be literal; calls cannot use for_each or retry.", InputSchema: json.RawMessage(`{"type":"object","properties":{"recipe_path":{"type":"string","minLength":1},"inputs":{"type":"object","maxProperties":32}},"required":["recipe_path"],"additionalProperties":false}`), Resources: resources, Execute: func(context.Context, json.RawMessage) (string, error) {
			return "", fmt.Errorf("workflow_call must be compiled before execution")
		}},
		{Name: "workflow_bind", Validate: validateBinding, Internal: true, PermissionName: "workflow_call", Version: 1, InputSchema: json.RawMessage(`{"type":"object","properties":{"recipe_path":{"type":"string"},"specs":{"type":"object"},"values":{"type":"object"}},"required":["recipe_path","specs","values"],"additionalProperties":false}`), Resources: resources, Execute: func(_ context.Context, raw json.RawMessage) (string, error) {
			var in Binding
			if err := json.Unmarshal(raw, &in); err != nil {
				return "", err
			}
			values, err := recipe.BindInputs(in.Specs, in.Values)
			if err != nil {
				return "", err
			}
			out, err := json.Marshal(values)
			return string(out), err
		}},
		{Name: "workflow_value", Internal: true, Version: 1, InputSchema: json.RawMessage(`{"type":"object"}`), Resources: func(json.RawMessage) ([]string, error) { return nil, nil }, Execute: func(_ context.Context, raw json.RawMessage) (string, error) { return string(raw), nil }},
	}
}
