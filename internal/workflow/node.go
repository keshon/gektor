package workflow

import (
	"context"
	"encoding/json"
	"errors"
)

// Node describes one executable operation. Permission names remain stable even
// when display names or recipe versions change.
type Node struct {
	// Internal nodes are compiler-generated and omitted from authoring schemas.
	Internal       bool
	PermissionName string
	// RetrySafe declares that repeating a failed execution cannot duplicate effects.
	RetrySafe bool
	// Concurrent permits overlapping invocations. Maps require it; bounded workers
	// serialize operations without it by name across their jobs.
	Concurrent       bool
	Name             string
	Version          int
	Description      string
	InputSchema      json.RawMessage
	OutputSchema     json.RawMessage
	ReadOnly         bool
	Resources        func(json.RawMessage) ([]string, error)
	Validate         func(json.RawMessage) error
	ValidateTemplate func(json.RawMessage) error
	Execute          func(context.Context, json.RawMessage) (string, error)
}

// ExecutionError marks a node's operation failure. It does not establish whether
// an external effect happened. Permission and output-contract failures use plain errors.
type ExecutionError struct {
	Code string
	Err  error
}

func (e *ExecutionError) Error() string { return e.Err.Error() }
func (e *ExecutionError) Unwrap() error { return e.Err }

// OperationFailure extracts typed data. Callers must still exclude checkpoint
// failures and cancellation of their execution context before routing the result.
func OperationFailure(err error) *Failure {
	var execution *ExecutionError
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &execution) {
		return nil
	}
	code := execution.Code
	if code == "" {
		code = OperationFailed
	}
	return &Failure{Code: code, Message: err.Error()}
}
