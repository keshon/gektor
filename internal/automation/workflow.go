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
	Version int    `json:"version"`
	Name    string `json:"name"`
	Steps   []Step `json:"steps"`
}
type StepResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}
type Record struct {
	RecipePath   string       `json:"recipe_path,omitempty"`
	RecipeSHA256 string       `json:"recipe_sha256,omitempty"`
	RecipeSource string       `json:"recipe_source,omitempty"`
	Definition   Definition   `json:"definition"`
	Digest       string       `json:"digest"`
	Status       string       `json:"status"`
	Started      time.Time    `json:"started"`
	Finished     *time.Time   `json:"finished,omitempty"`
	Steps        []StepResult `json:"steps"`
}
type Handler interface {
	Validate(Step) error
	Run(context.Context, Step) (string, error)
}

func Validate(d Definition, h Handler) error {
	if d.Version != 1 {
		return fmt.Errorf("workflow version must be 1")
	}
	if len(d.Name) == 0 || len(d.Name) > 120 {
		return fmt.Errorf("name must contain 1–120 bytes")
	}
	if len(d.Steps) == 0 || len(d.Steps) > 16 {
		return fmt.Errorf("workflow requires 1–16 sequential steps")
	}
	seen := map[string]bool{}
	for _, s := range d.Steps {
		if s.ID == "" || len(s.ID) > 64 || seen[s.ID] {
			return fmt.Errorf("step IDs must be unique and contain 1–64 bytes")
		}
		seen[s.ID] = true
		if err := h.Validate(s); err != nil {
			return fmt.Errorf("step %s: %w", s.ID, err)
		}
	}
	return nil
}

// Run validates every step before any effects. Save is called before each
// operation; a process crash leaves a running record, never a false success.
// There is deliberately no automatic retry of a step with possible effects.
func Run(ctx context.Context, d Definition, h Handler, save func(Record) error) (Record, error) {
	if err := Validate(d, h); err != nil {
		return Record{}, err
	}
	data, err := json.Marshal(d)
	if err != nil {
		return Record{}, err
	}
	r := Record{Definition: d, Digest: fmt.Sprintf("%x", sha256.Sum256(data)), Status: "running", Started: time.Now().UTC()}
	for _, s := range d.Steps {
		r.Steps = append(r.Steps, StepResult{ID: s.ID, Status: "pending"})
	}
	for i, s := range d.Steps {
		r.Steps[i].Status = "running"
		if err := save(r); err != nil {
			return r, fmt.Errorf("save before step: %w", err)
		}
		err := ctx.Err()
		var out string
		if err == nil {
			out, err = h.Run(ctx, s)
		}
		r.Steps[i].Output = out
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
	}
	r.Status = "completed"
	now := time.Now().UTC()
	r.Finished = &now
	if err := save(r); err != nil {
		return r, fmt.Errorf("save completion: %w", err)
	}
	return r, nil
}
