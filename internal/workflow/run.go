package workflow

import (
	"encoding/json"
	"time"
)

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
	ScheduleID   string                     `json:"schedule_id,omitempty"`
	ScheduledFor *time.Time                 `json:"scheduled_for,omitempty"`
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
