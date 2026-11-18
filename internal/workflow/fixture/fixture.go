// Package fixture matches recorded operation calls without executing their tools.
package fixture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"

	"github.com/keshon/gektor/internal/workflow"
)

const MaxBytes = 4 << 20

type specification struct {
	Version int                        `json:"version"`
	Inputs  map[string]json.RawMessage `json:"inputs"`
	Calls   []call                     `json:"calls"`
	Expect  expectation                `json:"expect"`
}

type expectation struct {
	Status  string          `json:"status"`
	Outputs json.RawMessage `json:"outputs"`
}

type call struct {
	ID        string          `json:"step_id"`
	Operation string          `json:"operation"`
	Version   int             `json:"node_version"`
	Args      json.RawMessage `json:"args"`
	Responses []response      `json:"responses"`
}

type response struct {
	Output  json.RawMessage   `json:"output,omitempty"`
	Failure *workflow.Failure `json:"failure,omitempty"`
}

// Session owns attempt consumption for one test, including concurrent map workers.
type Session struct {
	// Inputs are captured values, borrowed read-only by the engine.
	Inputs map[string]json.RawMessage

	mu    sync.Mutex // guards consumed response counts
	spec  specification
	calls map[string]call
	used  map[string]int
}

// New decodes a private copy of the specification.
func New(raw []byte) (*Session, error) {
	if len(raw) > MaxBytes {
		return nil, fmt.Errorf("fixture exceeds 4 MiB")
	}

	var spec specification
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&spec)
	if err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("expected one fixture object")
	}

	if spec.Version != 1 || (spec.Expect.Status != "completed" && spec.Expect.Status != "failed") {
		return nil, fmt.Errorf("fixture requires version 1 and expected status completed or failed")
	}
	if len(spec.Calls) > workflow.MaxInvocations {
		return nil, fmt.Errorf("fixture exceeds the invocation budget")
	}

	session := &Session{
		Inputs: spec.Inputs,
		spec:   spec,
		calls:  map[string]call{},
		used:   map[string]int{},
	}
	for _, call := range spec.Calls {
		if call.ID == "" || call.Operation == "" || call.Version < 1 {
			return nil, fmt.Errorf("fixture call requires step_id, operation and node_version")
		}
		if !json.Valid(call.Args) {
			return nil, fmt.Errorf("fixture arguments for %s must be JSON", call.ID)
		}
		if len(call.Responses) == 0 || len(call.Responses) > 3 {
			return nil, fmt.Errorf("fixture call %s requires 1–3 responses", call.ID)
		}
		if Pure(call.Operation) {
			return nil, fmt.Errorf("fixture cannot replace pure operation %s", call.Operation)
		}
		if _, exists := session.calls[call.ID]; exists {
			return nil, fmt.Errorf("duplicate fixture step_id %q", call.ID)
		}

		for _, response := range call.Responses {
			hasOutput := len(response.Output) > 0
			hasFailure := response.Failure != nil
			if hasOutput == hasFailure {
				return nil, fmt.Errorf("fixture response requires exactly one output or failure")
			}
			if response.Failure != nil && (response.Failure.Code == "" || response.Failure.Message == "") {
				return nil, fmt.Errorf("fixture failure requires code and message")
			}
		}
		session.calls[call.ID] = call
	}
	return session, nil
}

// Pure identifies the fixed builtins the fixture runner executes for real.
func Pure(operation string) bool {
	return operation == "script_transform" || operation == "workflow_value" || operation == "workflow_call"
}

func (s *Session) Run(ctx context.Context, step workflow.Step) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := ctx.Err()
	if err != nil {
		return "", err
	}

	call, exists := s.calls[step.ID]
	if !exists {
		return "", fmt.Errorf("missing fixture for %s (%s)", step.ID, step.Tool)
	}
	version := step.NodeVersion
	if version == 0 {
		version = 1
	}
	if call.Operation != step.Tool || call.Version != version {
		return "", fmt.Errorf("fixture operation or version changed for %s", step.ID)
	}
	matches, err := sameJSON(call.Args, step.Args)
	if err != nil {
		return "", fmt.Errorf("fixture arguments for %s: %w", step.ID, err)
	}
	if !matches {
		return "", fmt.Errorf("fixture arguments changed for %s", step.ID)
	}

	index := s.used[step.ID]
	if index >= len(call.Responses) {
		return "", fmt.Errorf("fixture responses exhausted for %s", step.ID)
	}
	s.used[step.ID] = index + 1
	response := call.Responses[index]
	if response.Failure != nil {
		return "", &workflow.ExecutionError{Code: response.Failure.Code, Err: errors.New(response.Failure.Message)}
	}
	return string(response.Output), nil
}

// Check requires every supplied response to be used and compares declared outputs.
// Numeric literals stay exact; object property order and whitespace do not matter.
func (s *Session) Check(status string, outputs json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, call := range s.spec.Calls {
		if s.used[call.ID] != len(call.Responses) {
			return fmt.Errorf("unused fixture responses for %s", call.ID)
		}
	}

	if status != s.spec.Expect.Status {
		return fmt.Errorf("fixture expected status %s, got %s", s.spec.Expect.Status, status)
	}

	matches, err := sameJSON(outputs, s.spec.Expect.Outputs)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("fixture declared outputs differ from expected values")
	}
	return nil
}

func sameJSON(left, right json.RawMessage) (bool, error) {
	values := make([]any, 2)
	for index, raw := range []json.RawMessage{left, right} {
		if len(raw) == 0 {
			raw = json.RawMessage(`null`)
		}

		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		err := decoder.Decode(&values[index])
		if err != nil {
			return false, err
		}
		if decoder.Decode(new(any)) != io.EOF {
			return false, fmt.Errorf("expected one JSON value")
		}
	}
	return reflect.DeepEqual(values[0], values[1]), nil
}
