package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
)

// Repeats keep one child scope per iteration. Completed effects are never replayed
// when a later child pauses for review; the coordinator alone advances the inputs.
func executeRepeat(ctx context.Context, s workflow.Step, h workflow.Handler, r *workflow.Run, index int, save func(workflow.Run, ...int) error, scope executionScope) (string, error) {
	policy := *s.Repeat
	var input workflow.CallInput
	err := json.Unmarshal(s.Args, &input)
	if err != nil {
		return "", err
	}
	if len(r.Steps[index].Iterations) > policy.MaxIterations {
		return "", fmt.Errorf("repeat checkpoint exceeds max_iterations")
	}
	s.Repeat, s.Review = nil, false
	for n := 0; n < policy.MaxIterations; n++ {
		err = ctx.Err()
		if err != nil {
			return "", err
		}
		if n == len(r.Steps[index].Iterations) {
			if *scope.remaining < 1 {
				return "", fmt.Errorf("repeat exceeds remaining invocation slots")
			}
			if n > 0 && policy.DelayMS > 0 {
				timer := time.NewTimer(time.Duration(policy.DelayMS) * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return "", ctx.Err()
				case <-timer.C:
				}
			}

			var args json.RawMessage
			args, err = json.Marshal(input)
			if err != nil {
				return "", err
			}
			if len(args) > recipe.MaxArguments {
				return "", fmt.Errorf("repeat child inputs exceed 64 KiB")
			}
			*scope.remaining -= 1
			item := workflow.StepResult{ID: fmt.Sprintf("%s[%d]", s.ID, n), Status: "pending", Args: args}
			r.Steps[index].Iterations = append(r.Steps[index].Iterations, item)
			err = save(*r, index, n)
			if err != nil {
				return "", err
			}
		}

		item := r.Steps[index].Iterations[n]
		if item.Status != "completed" {
			if item.Status != "pending" && item.Status != "waiting_review" && item.Status != "waiting" {
				return "", fmt.Errorf("repeat item %s cannot resume from %s", item.ID, item.Status)
			}
			call := s
			call.ID, call.Args = item.ID, item.Args
			err = h.Validate(call)
			if err != nil {
				r.Steps[index].Iterations[n].Status = "failed"
				r.Steps[index].Iterations[n].Error = err.Error()
				return "", err
			}
			itemRun := workflow.Run{Status: "running", Steps: []workflow.StepResult{item}}
			persist := func(child workflow.Run, path ...int) error {
				r.Steps[index].Iterations[n] = child.Steps[0]
				if child.Paused() {
					r.Status, r.Steps[index].Status = child.Status, child.Status
					r.WakeAt = child.WakeAt
				}
				parentPath := []int{index, n}
				if len(path) > 0 {
					parentPath = append(parentPath, path[1:]...)
				}
				return save(*r, parentPath...)
			}
			var output string
			output, err = executeChild(ctx, call, h, &itemRun, 0, persist, scope)
			if err == nil && itemRun.Paused() {
				return "", nil
			}
			item = itemRun.Steps[0]
			item.Output, item.Status = output, "completed"
			if err != nil {
				item.Status, item.Error = "failed", err.Error()
				if ctx.Err() != nil {
					item.Status = "cancelled"
				}
			}
			r.Steps[index].Iterations[n] = item
			saveErr := save(*r, index, n)
			if saveErr != nil {
				return "", fmt.Errorf("save repeat item: %w", saveErr)
			}
			if err != nil {
				return "", err
			}
		}

		raw := []byte(item.Output)
		var done json.RawMessage
		done, err = recipe.OutputAt(raw, policy.Until)
		if err != nil {
			return "", fmt.Errorf("repeat until: %w", err)
		}
		var stop bool
		err = json.Unmarshal(done, &stop)
		if err != nil || string(done) == "null" {
			return "", fmt.Errorf("repeat until must select a boolean")
		}
		if stop {
			return item.Output, nil
		}
		if n+1 == policy.MaxIterations {
			return "", fmt.Errorf("repeat reached max_iterations (%d) before until became true", policy.MaxIterations)
		}
		var next json.RawMessage
		next, err = recipe.OutputAt(raw, policy.NextInputs)
		if err != nil {
			return "", fmt.Errorf("repeat next_inputs: %w", err)
		}
		input.Inputs = nil
		err = json.Unmarshal(next, &input.Inputs)
		if err != nil || input.Inputs == nil {
			return "", fmt.Errorf("repeat next_inputs must select an input object")
		}
	}
	return "", fmt.Errorf("repeat has no iterations")
}
