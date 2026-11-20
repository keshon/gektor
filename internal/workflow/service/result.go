package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

// DeclaredOutputs returns the compiler-collected, completed recipe outputs.
func DeclaredOutputs(r workflow.Run) json.RawMessage {
	for _, s := range r.Steps {
		if s.ID == "@outputs" && s.Status == "completed" {
			return json.RawMessage(s.Output)
		}
	}

	return nil
}

// Result retains the available outcome even when execution returns an error.
// Its zero value has no outcome. Valid describes validation-only calls. Steps is nil
// for continuations, whose complete execution evidence remains in the saved run.
type Result struct {
	RunID        string                  `json:"run_id,omitempty"`
	DatabasePath string                  `json:"database_path,omitempty"`
	Status       string                  `json:"status,omitempty"`
	RecipeSHA256 string                  `json:"recipe_sha256,omitempty"`
	Steps        []StepSummary           `json:"steps,omitzero"`
	Outputs      json.RawMessage         `json:"outputs,omitempty"`
	Callback     *workflow.CallbackState `json:"callback,omitempty"`
	ResumeAt     *time.Time              `json:"resume_at,omitempty"`
	Valid        bool                    `json:"valid,omitempty"`
	RecipePath   string                  `json:"recipe_path,omitempty"`
	Scope        string                  `json:"scope,omitempty"`
}

// StepSummary carries compact execution evidence; the saved run owns full output.
type StepSummary struct {
	FailureStage string          `json:"failure_stage,omitempty"`
	Recovery     string          `json:"recovery,omitempty"`
	ID           string          `json:"id"`
	Status       string          `json:"status"`
	Error        string          `json:"error,omitempty"`
	Sources      json.RawMessage `json:"sources,omitempty"`
	Items        json.RawMessage `json:"items,omitempty"`
}

// decode accepts one JSON value, rejecting accidental concatenated arguments.
func decode(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}

	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON object")
	}

	return nil
}
