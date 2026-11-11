// Package engine executes a frozen definition through an operation handler.
// The caller supplies authorization and persistence. Checkpoints precede
// effects, and an uncertain operation is never automatically replayed.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
// There is deliberately no automatic retry of a step with possible effects.
func Run(ctx context.Context, d workflow.Definition, h workflow.Handler, save func(workflow.Run) error) (workflow.Run, error) {
	return RunWithInputs(ctx, d, nil, h, save)
}

func RunWithInputs(ctx context.Context, d workflow.Definition, inputs map[string]json.RawMessage, h workflow.Handler, save func(workflow.Run) error) (workflow.Run, error) {
	return run(ctx, d, inputs, h, save, nil)
}

// Resume only continues an explicit review boundary; uncertain effects are never replayed.
func Resume(ctx context.Context, previous workflow.Run, h workflow.Handler, save func(workflow.Run) error) (workflow.Run, error) {
	if previous.Status != "waiting_review" {
		return previous, fmt.Errorf("run is not waiting for review")
	}

	r, err := run(ctx, previous.Definition, previous.Inputs, h, save, &previous)
	if err != nil && r.Status == "" {
		return previous, err
	}

	return r, err
}
func run(ctx context.Context, d workflow.Definition, inputs map[string]json.RawMessage, h workflow.Handler, save func(workflow.Run) error, previous *workflow.Run) (workflow.Run, error) {

	data, err := json.Marshal(d)
	if err != nil {
		return workflow.Run{}, err
	}

	// Freeze caller-owned maps and argument slices for the execution record.
	var frozen workflow.Definition
	if err := json.Unmarshal(data, &frozen); err != nil {
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
		r.Finished = nil
	}

	status := map[string]string{}

	for i, s := range prepared.Steps {
		if previous != nil && (r.Steps[i].Status == "completed" || r.Steps[i].Status == "skipped") {
			status[s.ID] = r.Steps[i].Status
			if r.Steps[i].Status == "completed" {
				outputs[s.ID] = r.Steps[i].Output
			}

			continue
		}

		err := ctx.Err()
		skip := false
		completed := 0
		dependencies := 0

		for _, dep := range s.DependsOn {
			if dep == s.Gate {
				continue
			}

			dependencies++
			if status[dep] == "completed" {
				completed++
			} else {
				skip = true
			}
		}

		if s.Merge == "any" && dependencies > 0 {
			skip = completed == 0
		}

		if s.CollectOutputs {
			skip = false
		}

		if s.Gate != "" && status[s.Gate] != "completed" {
			skip = true
		}

		if !skip && err == nil && len(s.When) > 0 {
			var condition json.RawMessage
			condition, err = recipe.ResolveOutputs(s.When, outputs)
			if err == nil {
				var value bool
				if e := json.Unmarshal(condition, &value); e != nil || string(condition) == "null" {
					err = fmt.Errorf("step %s condition must resolve to boolean", s.ID)
				} else {
					skip = !value
				}
			}
		}

		if skip && err == nil {
			r.Steps[i].Status = "skipped"
			status[s.ID] = "skipped"
			if err := save(r); err != nil {
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

		if err == nil && s.Review && !r.Approved[s.ID] {
			r.Status = "waiting_review"
			r.Steps[i].Status = "waiting_review"
			if err := save(r); err != nil {
				return r, err
			}

			return r, nil
		}

		r.Steps[i].Status = "running"
		if err := save(r); err != nil {
			return r, fmt.Errorf("save before step: %w", err)
		}

		var out string
		if err == nil {
			if len(s.ForEach) > 0 {
				out, err = executeIterations(ctx, s, h, &r, i, save)
			} else {
				out, err = executeAttempts(ctx, s, h, &r, i, save)
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
			if e := save(r); e != nil {
				return r, fmt.Errorf("step failed (%v); save result: %w", err, e)
			}

			return r, fmt.Errorf("step %s: %w", s.ID, err)
		}

		r.Steps[i].Status = "completed"
		status[s.ID] = "completed"
		outputs[s.ID] = out
		// Commit the outcome before preparing the next operation. A crash or
		// validation failure must not hide an already completed effect.
		if err := save(r); err != nil {
			return r, fmt.Errorf("save step %s result: %w", s.ID, err)
		}
	}

	r.Status = "completed"
	now := time.Now().UTC()
	r.Finished = &now
	if err := save(r); err != nil {
		return r, fmt.Errorf("save completion: %w", err)
	}

	return r, nil
}
