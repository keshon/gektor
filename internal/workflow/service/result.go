package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

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

type stepSummary struct {
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
