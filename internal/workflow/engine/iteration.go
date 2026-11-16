package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
)

type iterationUpdate struct {
	index int
	step  workflow.StepResult
	err   error
	done  bool
	ack   chan error
}

type iterationOutcome struct {
	Failure *workflow.Failure `json:"failure,omitempty"`
	Index   int               `json:"index"`
	Status  string            `json:"status"`
	Output  json.RawMessage   `json:"output,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// The coordinator alone updates the run. Workers wait for checkpoint acknowledgement
// before effects, and every started worker is joined even after persistence fails.
func executeIterations(ctx context.Context, s workflow.Step, h workflow.Handler, r *workflow.Run, index int, save func(workflow.Run, ...int) error, remaining *int) (string, error) {
	var items []json.RawMessage
	err := json.Unmarshal(s.ForEach, &items)
	if err != nil || string(s.ForEach) == "null" || len(items) > workflow.MaxIterations {
		return "", fmt.Errorf("invalid iteration arguments")
	}
	concurrency := max(1, s.Concurrency)
	if concurrency > workflow.MaxConcurrency {
		return "", fmt.Errorf("concurrency exceeds %d", workflow.MaxConcurrency)
	}

	// The coordinator reserves the array against the budget shared by all scopes.
	collect := s.OnError == "collect"
	s.ForEach, s.Concurrency, s.OnError = nil, 0, ""
	if r.Steps[index].Iterations == nil {
		if len(items) > *remaining {
			return "", fmt.Errorf("collection needs %d invocation slots; run has %d of %d available", len(items), max(0, *remaining), workflow.MaxInvocations)
		}
		*remaining -= len(items)
		r.Steps[index].Iterations = make([]workflow.StepResult, len(items))
		for n, args := range items {
			r.Steps[index].Iterations[n] = workflow.StepResult{
				ID: fmt.Sprintf("%s[%d]", s.ID, n), Status: "pending", Args: args,
			}
		}
		err = save(*r, index)
		if err != nil {
			return "", err
		}
	}
	if len(r.Steps[index].Iterations) != len(items) {
		return "", fmt.Errorf("collection checkpoint does not match inputs")
	}

	workctx, cancel := context.WithCancel(ctx)
	defer cancel()
	updates := make(chan iterationUpdate, concurrency)
	outcomes := make([]iterationOutcome, len(items))
	active, next, size := 0, 0, 0
	var workers sync.WaitGroup
	var failure error

	for next < len(items) || active > 0 {
		for failure == nil && next < len(items) && active < concurrency {
			item := r.Steps[index].Iterations[next]
			if item.Status == "completed" || item.Status == "skipped" || (item.Status == "failed" && collect) {
				outcome, encodedSize, outcomeErr := retainedOutcome(next, item)
				if outcomeErr != nil || size+encodedSize > recipe.MaxOutput-1024 {
					failure = fmt.Errorf("retained iteration output is invalid or exceeds 1 MiB")
					cancel()
					break
				}
				outcomes[next] = outcome
				size += encodedSize
				next++
				continue
			}
			if item.Status == "failed" && item.Failure != nil {
				cause := errors.New(item.Failure.Message)
				failure = &workflow.ExecutionError{Code: item.Failure.Code, Err: cause}
				cancel()
				break
			}
			if item.Status != "pending" {
				failure = fmt.Errorf("iteration %s cannot resume from %s", item.ID, item.Status)
				cancel()
				break
			}
			err = workctx.Err()
			if err != nil {
				failure = err
				break
			}
			child := s
			child.ID = r.Steps[index].Iterations[next].ID
			child.Args = items[next]
			err = h.Validate(child)
			if err != nil {
				failure = err
				cancel()
				break
			}
			n := next
			item.Attempts = append([]workflow.Attempt(nil), item.Attempts...)
			workers.Go(func() { runIteration(workctx, child, item, h, n, updates) })
			next++
			active++
		}
		if active == 0 {
			break
		}

		update := <-updates
		r.Steps[index].Iterations[update.index] = update.step
		if update.done {
			active--
			outcome, encodedSize, encodeErr := retainedOutcome(update.index, update.step)
			size += encodedSize
			if failure == nil && (encodeErr != nil || size > recipe.MaxOutput-1024) {
				failure = fmt.Errorf("iteration output exceeds 1 MiB")
			}
			outcomes[update.index] = outcome
			var execution *workflow.ExecutionError
			canCollect := collect && errors.As(update.err, &execution) && workctx.Err() == nil && !errors.Is(update.err, context.Canceled)
			if failure == nil && update.err != nil && !canCollect {
				failure = update.err
			}
		}

		// Drain and record owned workers after an operation failure. Storage
		// failures stop dispatch and are never converted into item failures.
		saveErr := save(*r, index, update.index)
		if saveErr != nil {
			failure = errors.Join(failure, saveErr)
		}
		if failure != nil {
			cancel()
		}
		if update.ack != nil {
			update.ack <- saveErr
		}
	}

	workers.Wait()
	if failure != nil {
		for n := next; n < len(items); n++ {
			if r.Steps[index].Iterations[n].Status == "pending" {
				r.Steps[index].Iterations[n].Status = "skipped"
			}
		}
		return "", failure
	}
	data, err := json.Marshal(struct {
		Items []iterationOutcome `json:"items"`
	}{Items: outcomes})
	return string(data), err
}

func runIteration(ctx context.Context, s workflow.Step, previous workflow.StepResult, h workflow.Handler, index int, updates chan<- iterationUpdate) {
	previous.Status = "running"
	nested := workflow.Run{Steps: []workflow.StepResult{previous}}
	persist := func(r workflow.Run, _ ...int) error {
		step := r.Steps[0]
		step.Attempts = append([]workflow.Attempt(nil), step.Attempts...)
		ack := make(chan error, 1)
		updates <- iterationUpdate{index: index, step: step, ack: ack}
		return <-ack
	}
	output, err := executeAttempts(ctx, s, h, &nested, 0, persist)
	step := nested.Steps[0]
	step.Output = output
	step.Status = "completed"
	if err == nil && !json.Valid([]byte(output)) {
		err = fmt.Errorf("iteration output is not valid JSON")
	}
	if err != nil {
		step.Status, step.Error = "failed", err.Error()
		if ctx.Err() == nil {
			step.Failure = workflow.OperationFailure(err)
		}
		if ctx.Err() != nil {
			step.Status = "cancelled"
		}
	}
	if len(step.Output) > recipe.MaxOutput {
		step.Output = step.Output[:recipe.MaxOutput]
	}
	updates <- iterationUpdate{index: index, step: step, err: err, done: true}
}

func retainedOutcome(index int, step workflow.StepResult) (iterationOutcome, int, error) {
	outcome := iterationOutcome{Index: index, Status: step.Status, Error: step.Error, Failure: step.Failure}
	if step.Status == "completed" && !json.Valid([]byte(step.Output)) {
		return outcome, 0, fmt.Errorf("completed iteration has invalid JSON output")
	}
	if json.Valid([]byte(step.Output)) {
		outcome.Output = json.RawMessage(step.Output)
	}
	encoded, err := json.Marshal(outcome)
	return outcome, len(encoded) + 1, err
}
