// Package engine executes a frozen definition through an operation handler.
// The caller supplies authorization and persistence. Checkpoints precede
// effects, and an uncertain operation is never automatically replayed.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
)

func Validate(d workflow.Definition, h workflow.Handler) error {
	_, _, err := recipe.Prepare(d, nil, h)
	return err
}

// Run validates step templates before any effects. Save is called before each
// operation; a process crash leaves a running record, never a false success.
// Save receives the changed step/item indexes; an empty path saves the whole run.
// There is deliberately no automatic retry of a step with possible effects.
func Run(ctx context.Context, d workflow.Definition, h workflow.Handler, save func(workflow.Run, ...int) error) (workflow.Run, error) {
	return RunWithInputs(ctx, d, nil, h, save)
}

func RunWithInputs(ctx context.Context, d workflow.Definition, inputs map[string]json.RawMessage, h workflow.Handler, save func(workflow.Run, ...int) error) (workflow.Run, error) {
	remaining := workflow.MaxInvocations
	return run(ctx, d, inputs, h, save, nil, executionScope{remaining: &remaining})
}

// Resume continues reviews or due waits; uncertain effects are never replayed.
func Resume(ctx context.Context, previous workflow.Run, h workflow.Handler, save func(workflow.Run, ...int) error) (workflow.Run, error) {
	if !previous.Paused() {
		return previous, fmt.Errorf("run is not paused")
	}
	if previous.Status == "waiting" && (previous.WakeAt == nil || time.Now().Before(*previous.WakeAt)) {
		return previous, fmt.Errorf("wait deadline has not arrived")
	}

	remaining := workflow.MaxInvocations - invocationCount(previous)
	scope := executionScope{remaining: &remaining, approved: previous.Approved}
	r, err := run(ctx, previous.Definition, previous.Inputs, h, save, &previous, scope)
	if err != nil && r.Status == "" {
		return previous, err
	}

	return r, err
}
func run(ctx context.Context, d workflow.Definition, inputs map[string]json.RawMessage, h workflow.Handler, save func(workflow.Run, ...int) error, previous *workflow.Run, scope executionScope) (workflow.Run, error) {

	var checkpointErr error
	checkpoint := save
	save = func(record workflow.Run, path ...int) error {
		if checkpointErr != nil {
			return checkpointErr
		}
		err := checkpoint(record, path...)
		if err != nil {
			checkpointErr = err
		}
		return err
	}

	data, err := json.Marshal(d)
	if err != nil {
		return workflow.Run{}, err
	}

	// Freeze caller-owned maps and argument slices for the execution record.
	var frozen workflow.Definition
	err = json.Unmarshal(data, &frozen)
	if err != nil {
		return workflow.Run{}, err
	}

	d = frozen
	prepared, bound, err := recipe.Prepare(d, inputs, h)
	if err != nil {
		return workflow.Run{}, err
	}

	r := workflow.Run{
		Definition: d,
		Inputs:     bound,
		Digest:     fmt.Sprintf("%x", sha256.Sum256(data)),
		Status:     "running",
		Started:    time.Now().UTC(),
	}
	outputs := map[string]string{}

	for _, s := range prepared.Steps {
		r.Steps = append(r.Steps, workflow.StepResult{ID: s.ID, Status: "pending"})
	}

	if previous != nil {
		if previous.Digest != r.Digest || len(previous.Steps) != len(r.Steps) {
			return *previous, fmt.Errorf("run checkpoint does not match definition")
		}

		for i, s := range r.Steps {
			if previous.Steps[i].ID != s.ID {
				return *previous, fmt.Errorf("run checkpoint step mismatch")
			}
		}

		r = *previous
		r.Status = "running"
		r.Finished, r.WakeAt = nil, nil
	}

	if previous == nil {
		if len(r.Steps) > *scope.remaining {
			return r, fmt.Errorf("child scope exceeds run invocation budget")
		}
		*scope.remaining -= len(r.Steps)
	}
	err = save(r)
	if err != nil {
		return r, fmt.Errorf("save initial run: %w", err)
	}

	status := map[string]string{}
	failures := map[string]workflow.Failure{}
	var firstFailure error

	for i, s := range prepared.Steps {
		if previous != nil && r.Steps[i].Status == "failed" && r.Steps[i].Failure != nil {
			failure := *r.Steps[i].Failure
			failures[s.ID], status[s.ID] = failure, "failed"
			encoded, encodeErr := json.Marshal(workflow.FailureOutput{Error: failure})
			if encodeErr != nil {
				return r, encodeErr
			}
			outputs[s.ID] = string(encoded)
			if firstFailure == nil {
				cause := errors.New(failure.Message)
				execution := &workflow.ExecutionError{Code: failure.Code, Err: cause}
				firstFailure = fmt.Errorf("step %s: %w", s.ID, execution)
			}
			continue
		}
		if previous != nil && (r.Steps[i].Status == "completed" || r.Steps[i].Status == "skipped") {
			status[s.ID] = r.Steps[i].Status
			if r.Steps[i].Status == "completed" {
				outputs[s.ID] = r.Steps[i].Output
			}

			continue
		}

		err = ctx.Err()
		resumingWait := previous != nil && s.Tool == "workflow_wait" && r.Steps[i].Status == "waiting" && r.Steps[i].Wait != nil
		if err == nil && resumingWait {
			if time.Now().Before(r.Steps[i].Wait.ResumeAt) {
				return r, fmt.Errorf("wait deadline has not arrived")
			}
			r.Steps[i].Status = "completed"
			status[s.ID], outputs[s.ID] = "completed", r.Steps[i].Output
			err = save(r, i)
			if err != nil {
				return r, err
			}
			continue
		}
		skip := false
		completed := 0
		dependencies := 0

		for _, dep := range s.DependsOn {
			dependencies++
			if status[dep] == "completed" || s.Finally || (dep == s.OnFailure && status[dep] == "failed") {
				completed++
			} else {
				skip = true
			}
		}

		if s.Merge == "any" && dependencies > 0 {
			skip = completed == 0
		}

		if s.OnFailure != "" {
			_, failed := failures[s.OnFailure]
			skip = skip || !failed
		}
		if firstFailure != nil && !s.Finally && s.OnFailure == "" {
			skip = true
		}
		if s.CollectOutputs {
			skip = firstFailure != nil
		}

		if !skip && err == nil && len(s.When) > 0 {
			var condition json.RawMessage
			condition, err = recipe.ResolveOutputs(s.When, outputs)
			if err == nil {
				var value bool
				err = json.Unmarshal(condition, &value)
				if err != nil || string(condition) == "null" {
					err = fmt.Errorf("step %s condition must resolve to boolean", s.ID)
				} else {
					skip = !value
				}
			}
		}

		if skip && err == nil {
			r.Steps[i].Status = "skipped"
			status[s.ID] = "skipped"
			err = save(r, i)
			if err != nil {
				return r, err
			}

			continue
		}

		if err == nil {
			s.Args, err = recipe.ResolveOutputs(s.Args, outputs)
			if err == nil && len(s.ForEach) > 0 {
				s.ForEach, err = recipe.ResolveOutputs(s.ForEach, outputs)
			}
		}

		if err == nil {
			r.Steps[i].Args = s.Args
			r.Steps[i].ForEach = s.ForEach
			err = h.Validate(s)
		}

		if err == nil && s.Review && !scope.approved[scope.prefix+s.ID] {
			r.Status = "waiting_review"
			r.Steps[i].Status = "waiting_review"
			err = save(r, i)
			if err != nil {
				r.Status = "running"
				return r, err
			}

			return r, nil
		}

		r.Steps[i].Status = "running"
		saveErr := save(r, i)
		if saveErr != nil {
			return r, fmt.Errorf("save before step: %w", saveErr)
		}

		operation := s
		operation.ID = scope.prefix + s.ID
		var out string
		if err == nil {
			if s.Child != nil {
				if s.Repeat != nil {
					out, err = executeRepeat(ctx, operation, h, &r, i, save, scope)
				} else if len(s.ForEach) > 0 {
					out, err = executeChildIterations(ctx, operation, h, &r, i, save, scope)
				} else {
					out, err = executeChild(ctx, operation, h, &r, i, save, scope)
				}
				if err == nil && r.Paused() {
					return r, nil
				}
			} else if len(s.ForEach) > 0 {
				out, err = executeIterations(ctx, operation, h, &r, i, save, scope.remaining)
			} else if previous != nil && s.Tool == "workflow_wait_callback" && r.Steps[i].Callback != nil {
				pending := r.Steps[i].Callback
				if pending.EventID == "" {
					cause := errors.New("callback wait expired without an event")
					err = &workflow.ExecutionError{Code: "callback_expired", Err: cause}
				} else {
					var encoded []byte
					encoded, err = json.Marshal(workflow.CallbackResult{EventID: pending.EventID, Payload: pending.Payload})
					out = string(encoded)
				}
			} else {
				out, err = executeAttempts(ctx, operation, h, &r, i, save)
			}
		}

		r.Steps[i].Output = out
		if len(out) > recipe.MaxOutput {
			r.Steps[i].Output = out[:recipe.MaxOutput]
			if err == nil {
				err = fmt.Errorf("step output exceeds 1 MiB")
			}
		}

		if err == nil {
			err = ctx.Err()
		}

		if err == nil && s.Tool == "workflow_wait" {
			var wait workflow.WaitState
			err = json.Unmarshal([]byte(out), &wait)
			if err == nil && wait.ResumeAt.IsZero() {
				err = fmt.Errorf("wait returned no deadline")
			}
			if err == nil {
				r.Steps[i].Wait, r.Steps[i].Status = &wait, "waiting"
				r.Status, r.WakeAt = "waiting", &wait.ResumeAt
				err = save(r, i)
				if err == nil {
					return r, nil
				}
				r.WakeAt = nil
			}
		}
		if err == nil && s.Tool == "workflow_wait_callback" && r.Steps[i].Callback == nil {
			var pending workflow.CallbackState
			err = json.Unmarshal([]byte(out), &pending)
			if err == nil && (pending.ID == "" || pending.Channel == "" || pending.ExpiresAt.IsZero()) {
				err = fmt.Errorf("callback wait returned an invalid receipt")
			}
			if err == nil {
				pending.StepID = operation.ID
				r.Steps[i].Callback, r.Steps[i].Status, r.Steps[i].Output = &pending, "waiting", ""
				r.Status, r.WakeAt = "waiting", &pending.ExpiresAt
				err = save(r, i)
				if err == nil {
					return r, nil
				}
				r.WakeAt = nil
			}
		}
		if checkpointErr != nil {
			// Owned map workers have joined. Keep the last durable record available
			// for inspection instead of overwriting uncertain effects as failures.
			r.Status, r.Finished, r.WakeAt = "running", nil, nil
			return r, fmt.Errorf("checkpoint step %s: %w", s.ID, checkpointErr)
		}
		failure := workflow.OperationFailure(err)
		if failure != nil && ctx.Err() == nil {
			encoded, encodeErr := json.Marshal(workflow.FailureOutput{Error: *failure})
			if encodeErr != nil {
				return r, encodeErr
			}
			r.Steps[i].Failure = failure
			r.Steps[i].Status, r.Steps[i].Error = "failed", failure.Message
			status[s.ID], failures[s.ID], outputs[s.ID] = "failed", *failure, string(encoded)
			if firstFailure == nil {
				firstFailure = fmt.Errorf("step %s: %w", s.ID, err)
			}
			err = save(r, i)
			if err != nil {
				return r, fmt.Errorf("save operation failure: %w", err)
			}
			continue
		}
		if err != nil {
			r.Steps[i].Status = "failed"
			r.Steps[i].Error = err.Error()
			r.Status = "failed"
			if ctx.Err() != nil {
				r.Status = "cancelled"
				r.Steps[i].Status = "cancelled"
			}

			for j := i + 1; j < len(r.Steps); j++ {
				r.Steps[j].Status = "skipped"
			}

			now := time.Now().UTC()
			r.Finished = &now
			saveErr := save(r)
			if saveErr != nil {
				r.Status, r.Finished = "running", nil
				return r, fmt.Errorf("step failed (%v); save result: %w", err, saveErr)
			}

			return r, fmt.Errorf("step %s: %w", s.ID, err)
		}

		r.Steps[i].Status = "completed"
		status[s.ID] = "completed"
		outputs[s.ID] = out
		// Commit the outcome before preparing the next operation. A crash or
		// validation failure must not hide an already completed effect.
		err = save(r, i)
		if err != nil {
			return r, fmt.Errorf("save step %s result: %w", s.ID, err)
		}
	}

	r.Status = "completed"
	if firstFailure != nil {
		r.Status = "failed"
	}
	now := time.Now().UTC()
	r.Finished = &now
	err = save(r)
	if err != nil {
		r.Status, r.Finished = "running", nil
		return r, fmt.Errorf("save completion: %w", err)
	}

	return r, firstFailure
}
