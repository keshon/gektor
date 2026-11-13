package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
)

// Child calls are serial: the active scope owns its workers and checkpoints.
// Review pauses retain the current child and all undispatched item reservations.
func executeChildIterations(ctx context.Context, s workflow.Step, h workflow.Handler, r *workflow.Run, index int, save func(workflow.Run, ...int) error, scope executionScope) (string, error) {
	var inputs []json.RawMessage
	err := json.Unmarshal(s.ForEach, &inputs)
	if err != nil || string(s.ForEach) == "null" || len(inputs) > workflow.MaxIterations {
		return "", fmt.Errorf("invalid mapped child inputs")
	}
	if s.Concurrency > 1 {
		return "", fmt.Errorf("child calls execute serially")
	}
	if r.Steps[index].Iterations == nil {
		if len(inputs) > *scope.remaining {
			return "", fmt.Errorf("mapped calls exceed remaining invocation slots")
		}
		*scope.remaining -= len(inputs)
		items := make([]workflow.StepResult, len(inputs))
		for n, input := range inputs {
			var call workflow.Step
			call, err = recipe.CallIteration(s, input)
			if err != nil {
				return "", err
			}
			items[n] = workflow.StepResult{ID: fmt.Sprintf("%s[%d]", s.ID, n), Status: "pending", Args: call.Args}
		}
		r.Steps[index].Iterations = items
		err = save(*r, index)
		if err != nil {
			return "", err
		}
	}
	if len(r.Steps[index].Iterations) != len(inputs) {
		return "", fmt.Errorf("mapped call checkpoint does not match inputs")
	}

	outcomes := make([]iterationOutcome, len(inputs))
	size := 0
	paused := false
	defer func() {
		if paused {
			return
		}
		for n := range r.Steps[index].Iterations {
			if r.Steps[index].Iterations[n].Status == "pending" {
				r.Steps[index].Iterations[n].Status = "skipped"
			}
		}
	}()
	for n := range inputs {
		item := r.Steps[index].Iterations[n]
		if item.Status != "completed" && item.Status != "failed" && item.Status != "skipped" {
			err = ctx.Err()
			if err != nil {
				return "", err
			}
			call := s
			call.ID, call.Args = item.ID, item.Args
			call.ForEach, call.Concurrency, call.OnError, call.Review = nil, 0, "", false
			err = h.Validate(call)
			if err != nil {
				return "", err
			}
			itemRun := workflow.Run{Status: "running", Steps: []workflow.StepResult{item}}
			persist := func(child workflow.Run, path ...int) error {
				r.Steps[index].Iterations[n] = child.Steps[0]
				if child.Status == "waiting_review" {
					r.Status, r.Steps[index].Status = "waiting_review", "waiting_review"
				}
				parentPath := []int{index, n}
				if len(path) > 0 {
					parentPath = append(parentPath, path[1:]...)
				}
				return save(*r, parentPath...)
			}
			var output string
			output, err = executeChild(ctx, call, h, &itemRun, 0, persist, scope)
			if err == nil && itemRun.Status == "waiting_review" {
				paused = true
				return "", nil
			}
			if err == nil && !json.Valid([]byte(output)) {
				err = fmt.Errorf("child output is not valid JSON")
			}
			item = itemRun.Steps[0]
			item.Output, item.Status = output, "completed"
			if err != nil {
				item.Status, item.Error = "failed", err.Error()
			}
			if err != nil && ctx.Err() != nil {
				item.Status = "cancelled"
			}
			r.Steps[index].Iterations[n] = item
			saveErr := save(*r, index, n)
			if saveErr != nil {
				return "", fmt.Errorf("save child item: %w", saveErr)
			}
			var execution *workflow.ExecutionError
			canCollect := s.OnError == "collect" && errors.As(err, &execution) && ctx.Err() == nil && !errors.Is(err, context.Canceled)
			if err != nil && !canCollect {
				return "", err
			}
		}
		outcome := iterationOutcome{Index: n, Status: item.Status, Error: item.Error}
		if json.Valid([]byte(item.Output)) {
			outcome.Output = json.RawMessage(item.Output)
		}
		encoded, encodeErr := json.Marshal(outcome)
		if encodeErr != nil {
			return "", encodeErr
		}
		size += len(encoded) + 1
		if size > recipe.MaxOutput-1024 {
			return "", fmt.Errorf("mapped child output exceeds 1 MiB")
		}
		outcomes[n] = outcome
	}
	encoded, err := json.Marshal(struct {
		Items []iterationOutcome `json:"items"`
	}{Items: outcomes})
	return string(encoded), err
}
