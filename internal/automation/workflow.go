// Package automation runs versioned recipes independently of model turns.
package automation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

type Step struct {
	ID   string          `json:"id"`
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}
type Definition struct {
	Version int              `json:"version"`
	Name    string           `json:"name"`
	Inputs  map[string]Input `json:"inputs,omitempty"`
	Steps   []Step           `json:"steps"`
}
type StepResult struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Args   json.RawMessage `json:"args,omitempty"`
	Output string          `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
}
type Record struct {
	RunID        string                     `json:"run_id,omitempty"`
	Workspace    string                     `json:"workspace,omitempty"`
	RecipePath   string                     `json:"recipe_path,omitempty"`
	RecipeSHA256 string                     `json:"recipe_sha256,omitempty"`
	RecipeSource string                     `json:"recipe_source,omitempty"`
	Definition   Definition                 `json:"definition"`
	Inputs       map[string]json.RawMessage `json:"inputs,omitempty"`
	Digest       string                     `json:"digest"`
	Status       string                     `json:"status"`
	Started      time.Time                  `json:"started"`
	Finished     *time.Time                 `json:"finished,omitempty"`
	Steps        []StepResult               `json:"steps"`
}
type Handler interface {
	Validate(Step) error
	Run(context.Context, Step) (string, error)
}

func Validate(d Definition, h Handler) error {
	_, _, err := Prepare(d, nil, h)
	return err
}

// Run validates step templates before any effects. Save is called before each
// operation; a process crash leaves a running record, never a false success.
// There is deliberately no automatic retry of a step with possible effects.
func Run(ctx context.Context, d Definition, h Handler, save func(Record) error) (Record, error) {
	return RunWithInputs(ctx, d, nil, h, save)
}

func RunWithInputs(ctx context.Context, d Definition, inputs map[string]json.RawMessage, h Handler, save func(Record) error) (Record, error) {
	data, err := json.Marshal(d)
	if err != nil {
		return Record{}, err
	}
	// Freeze caller-owned maps and argument slices for the execution record.
	var frozen Definition
	if err := json.Unmarshal(data, &frozen); err != nil {
		return Record{}, err
	}
	d = frozen
	prepared, bound, err := Prepare(d, inputs, h)
	if err != nil {
		return Record{}, err
	}
	r := Record{Definition: d, Inputs: bound, Digest: fmt.Sprintf("%x", sha256.Sum256(data)), Status: "running", Started: time.Now().UTC()}
	outputs := map[string]string{}
	for _, s := range d.Steps {
		r.Steps = append(r.Steps, StepResult{ID: s.ID, Status: "pending"})
	}
	for i, s := range prepared.Steps {
		err := ctx.Err()
		if err == nil && d.Version == 2 {
			s.Args, err = resolveOutputs(s.Args, outputs)
		}
		if err == nil {
			r.Steps[i].Args = s.Args
			err = h.Validate(s)
		}
		r.Steps[i].Status = "running"
		if err := save(r); err != nil {
			return r, fmt.Errorf("save before step: %w", err)
		}
		var out string
		if err == nil {
			out, err = h.Run(ctx, s)
		}
		r.Steps[i].Output = out
		if len(out) > maxOutput {
			r.Steps[i].Output = out[:maxOutput]
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
		outputs[s.ID] = out
	}
	r.Status = "completed"
	now := time.Now().UTC()
	r.Finished = &now
	if err := save(r); err != nil {
		return r, fmt.Errorf("save completion: %w", err)
	}
	return r, nil
}
