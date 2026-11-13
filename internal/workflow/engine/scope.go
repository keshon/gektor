package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/workflow"
)

// Scopes execute serially. Only the active coordinator reserves invocation slots;
// leaf workers never mutate this budget or the enclosing run tree.
type executionScope struct {
	remaining *int
	approved  map[string]bool
	prefix    string
}

func invocationCount(r workflow.Run) int {
	count := 0
	var visit func([]workflow.StepResult)
	visit = func(steps []workflow.StepResult) {
		count += len(steps)
		for _, step := range steps {
			visit(step.Iterations)
			if step.Child != nil {
				visit(step.Child.Steps)
			}
		}
	}
	visit(r.Steps)
	return count
}

func executeChild(ctx context.Context, s workflow.Step, h workflow.Handler, r *workflow.Run, index int, save func(workflow.Run, ...int) error, scope executionScope) (string, error) {
	previous := r.Steps[index].Child
	var inputs map[string]json.RawMessage
	if previous == nil {
		bound, err := executeAttempts(ctx, s, h, r, index, save)
		if err != nil {
			return "", err
		}
		err = json.Unmarshal([]byte(bound), &inputs)
		if err != nil {
			return "", fmt.Errorf("child input binding: %w", err)
		}
	} else {
		// Recheck the call permission on continuation. Binding is pure; the
		// child's persisted inputs and completed effects remain authoritative.
		_, err := h.Run(ctx, s)
		if err != nil {
			return "", err
		}
		inputs = previous.Inputs
	}
	scope.prefix = s.ID + "/"
	persist := func(child workflow.Run, path ...int) error {
		r.Steps[index].Child = &child
		if child.Status == "waiting_review" {
			r.Status, r.Steps[index].Status = "waiting_review", "waiting_review"
		} else {
			r.Steps[index].Status = "running"
		}
		if len(path) == 0 {
			return save(*r, index)
		}
		parentPath := []int{index, workflow.ChildScope}
		parentPath = append(parentPath, path...)
		return save(*r, parentPath...)
	}
	child, err := run(ctx, *s.Child, inputs, h, persist, previous, scope)
	if err != nil {
		return "", err
	}
	if child.Status == "waiting_review" {
		return "", nil
	}
	for _, step := range child.Steps {
		if step.ID == "@outputs" && step.Status == "completed" {
			return step.Output, nil
		}
	}
	return "", fmt.Errorf("child scope has no completed outputs")
}
