package automation

import (
	"context"
	"encoding/json"
	"time"
)

type Step struct {
	// CollectOutputs is compiler-owned: dependencies order collection but skipped
	// branches do not suppress it. Missing referenced outputs still fail resolution.
	CollectOutputs bool `json:"collect_outputs,omitempty"`
	// Gate is compiler-owned; it must complete regardless of merge semantics.
	Gate        string          `json:"gate,omitempty"`
	Review      bool            `json:"review,omitempty"`
	ForEach     json.RawMessage `json:"for_each,omitempty"`
	NodeVersion int             `json:"node_version,omitempty"`
	Retry       *RetryPolicy    `json:"retry,omitempty"`
	ID          string          `json:"id"`
	Tool        string          `json:"tool"`
	Args        json.RawMessage `json:"args"`
	DependsOn   []string        `json:"depends_on,omitempty"`
	When        json.RawMessage `json:"when,omitempty"`
	Merge       string          `json:"merge,omitempty"`
}
type Definition struct {
	Outputs map[string]json.RawMessage `json:"outputs,omitempty"`
	Version int                        `json:"version"`
	Name    string                     `json:"name"`
	Inputs  map[string]Input           `json:"inputs,omitempty"`
	Steps   []Step                     `json:"steps"`
}
type RetryPolicy struct {
	MaxAttempts int `json:"max_attempts"`
	DelayMS     int `json:"delay_ms,omitempty"`
}
type Attempt struct {
	Number   int        `json:"number"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
	Error    string     `json:"error,omitempty"`
}
type StepResult struct {
	ForEach    json.RawMessage `json:"for_each,omitempty"`
	Iterations []StepResult    `json:"iterations,omitempty"`
	Attempts   []Attempt       `json:"attempts,omitempty"`

	ID     string          `json:"id"`
	Status string          `json:"status"`
	Args   json.RawMessage `json:"args,omitempty"`
	Output string          `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
}
type Run struct {
	Approved     map[string]bool            `json:"approved,omitempty"`
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
