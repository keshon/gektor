// Package workflow defines recipe, node, and execution contracts.
// Recipe preparation, execution, storage, and application coordination live in subpackages.
package workflow

import (
	"context"
	"encoding/json"
)

const MaxIterations = 10000
const MaxConcurrency = 32

// Definition is a saved recipe. Its maps and slices are caller-owned.
type Definition struct {
	Outputs map[string]json.RawMessage `json:"outputs,omitempty"`
	Version int                        `json:"version"`
	Name    string                     `json:"name"`
	Inputs  map[string]Input           `json:"inputs,omitempty"`
	Steps   []Step                     `json:"steps"`
}

// Step describes one recipe operation and its dependencies.
type Step struct {
	// CollectOutputs is compiler-owned: dependencies order collection but skipped
	// branches do not suppress it. Missing referenced outputs still fail resolution.
	CollectOutputs bool `json:"collect_outputs,omitempty"`
	// Gate is compiler-owned; it must complete regardless of merge semantics.
	Gate        string          `json:"gate,omitempty"`
	Review      bool            `json:"review,omitempty"`
	ForEach     json.RawMessage `json:"for_each,omitempty"`
	Concurrency int             `json:"concurrency,omitempty"`
	OnError     string          `json:"on_error,omitempty"`
	NodeVersion int             `json:"node_version,omitempty"`
	Retry       *RetryPolicy    `json:"retry,omitempty"`
	ID          string          `json:"id"`
	Tool        string          `json:"tool"`
	Args        json.RawMessage `json:"args"`
	DependsOn   []string        `json:"depends_on,omitempty"`
	When        json.RawMessage `json:"when,omitempty"`
	Merge       string          `json:"merge,omitempty"`
}
type RetryPolicy struct {
	MaxAttempts int `json:"max_attempts"`
	DelayMS     int `json:"delay_ms,omitempty"`
}
type Handler interface {
	Validate(Step) error
	Run(context.Context, Step) (string, error)
}
