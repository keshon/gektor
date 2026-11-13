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
	index  int
	step   workflow.StepResult
	output string
	err    error
	done   bool
	ack    chan error
}

type iterationOutcome struct {
	Index  int             `json:"index"`
	Status string          `json:"status"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// The coordinator alone updates the run. Workers wait for checkpoint acknowledgement
// before effects, and every started worker is joined even after persistence fails.
func executeIterations(ctx context.Context, s workflow.Step, h workflow.Handler, r *workflow.Run, index int, save func(workflow.Run, ...int) error) (string, error) {
	var items []json.RawMessage
	err := json.Unmarshal(s.ForEach, &items)
	if err != nil || string(s.ForEach) == "null" || len(items) > workflow.MaxIterations {
		return "", fmt.Errorf("invalid iteration arguments")
	}
	concurrency := max(1, s.Concurrency)
	if concurrency > workflow.MaxConcurrency {
		return "", fmt.Errorf("concurrency exceeds %d", workflow.MaxConcurrency)
	}

	// Reserve the entire collection before allocating records or starting effects.
	// Existing slots remain charged across review continuation, including failures
	// and items skipped after cancellation. The coordinator alone owns this tree.
	invocations := len(r.Steps)
	for _, step := range r.Steps {
		invocations += len(step.Iterations)
	}
	if len(items) > workflow.MaxInvocations-invocations {
		return "", fmt.Errorf("collection needs %d invocation slots; run has %d of %d available", len(items), max(0, workflow.MaxInvocations-invocations), workflow.MaxInvocations)
	}

	collect := s.OnError == "collect"
	s.ForEach, s.Concurrency, s.OnError = nil, 0, ""
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

	workctx, cancel := context.WithCancel(ctx)
	defer cancel()
	updates := make(chan iterationUpdate, concurrency)
	outcomes := make([]iterationOutcome, len(items))
	active, next, size := 0, 0, 0
	var workers sync.WaitGroup
	var failure error

	for next < len(items) || active > 0 {
		for failure == nil && next < len(items) && active < concurrency {
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
			workers.Go(func() { runIteration(workctx, child, h, n, updates) })
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
			outcome := iterationOutcome{Index: update.index, Status: update.step.Status, Error: update.step.Error}
			if json.Valid([]byte(update.output)) {
				outcome.Output = json.RawMessage(update.output)
			}
			encoded, encodeErr := json.Marshal(outcome)
			size += len(encoded) + 1
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
			r.Steps[index].Iterations[n].Status = "skipped"
		}
		return "", failure
	}
	data, err := json.Marshal(struct {
		Items []iterationOutcome `json:"items"`
	}{Items: outcomes})
	return string(data), err
}

func runIteration(ctx context.Context, s workflow.Step, h workflow.Handler, index int, updates chan<- iterationUpdate) {
	nested := workflow.Run{Steps: []workflow.StepResult{{ID: s.ID, Status: "running", Args: s.Args}}}
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
		if ctx.Err() != nil {
			step.Status = "cancelled"
		}
	}
	if len(step.Output) > recipe.MaxOutput {
		step.Output = step.Output[:recipe.MaxOutput]
	}
	updates <- iterationUpdate{index: index, step: step, output: output, err: err, done: true}
}
