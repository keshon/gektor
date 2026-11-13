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
	Child      *Run            `json:"child,omitempty"`
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

// ScopedStep borrows its definition and result from the run. Treat them as read-only.
type ScopedStep struct {
	ID         string
	Definition Step
	Result     StepResult
}

func ScopedSteps(r Run) []ScopedStep {
	var out []ScopedStep
	var visit func(Run, string)
	visit = func(scope Run, prefix string) {
		definitions := make(map[string]Step, len(scope.Definition.Steps))
		for _, step := range scope.Definition.Steps {
			definitions[step.ID] = step
		}
		for _, step := range scope.Steps {
			id := prefix + step.ID
			out = append(out, ScopedStep{ID: id, Definition: definitions[step.ID], Result: step})
			if step.Child != nil {
				visit(*step.Child, id+"/")
			}
			if definitions[step.ID].Child != nil {
				definition := definitions[step.ID]
				definition.ForEach, definition.Concurrency, definition.OnError, definition.Review = nil, 0, "", false
				for _, item := range step.Iterations {
					out = append(out, ScopedStep{ID: item.ID, Definition: definition, Result: item})
					if item.Child != nil {
						visit(*item.Child, item.ID+"/")
					}
				}
			}
		}
	}
	visit(r, "")
	return out
}
