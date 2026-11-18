package workflow

import (
	"encoding/json"
	"time"
)

type Attempt struct {
	Failure  *Failure   `json:"failure,omitempty"`
	Number   int        `json:"number"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
	Error    string     `json:"error,omitempty"`
}
type WaitState struct {
	ResumeAt time.Time `json:"resume_at"`
}

// CallbackState identifies one wait. The store hydrates event fields on pending
// steps after acceptance; completed steps retain the event in their output.
type CallbackState struct {
	ID        string          `json:"wait_id"`
	StepID    string          `json:"step_id"`
	Channel   string          `json:"channel"`
	ExpiresAt time.Time       `json:"expires_at"`
	EventID   string          `json:"event_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type CallbackResult struct {
	EventID string          `json:"event_id"`
	Payload json.RawMessage `json:"payload"`
}

func PendingCallback(r Run) *CallbackState {
	if r.Status != "waiting" {
		return nil
	}
	for _, step := range ScopedSteps(r) {
		if step.Result.Status == "waiting" && step.Result.Callback != nil {
			pending := *step.Result.Callback
			pending.EventID, pending.Payload = "", nil
			return &pending
		}
	}
	return nil
}

type StepResult struct {
	Failure    *Failure        `json:"failure,omitempty"`
	Callback   *CallbackState  `json:"callback,omitempty"`
	Wait       *WaitState      `json:"wait,omitempty"`
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

const OperationFailed = "operation_failed"

// Failure is data from a known operation failure, never a permission or checkpoint error.
type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type FailureOutput struct {
	Error Failure `json:"error"`
}
type Run struct {
	Recoveries   []RecoveryRecord           `json:"recoveries,omitempty"`
	WakeAt       *time.Time                 `json:"wake_at,omitempty"`
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

type Recovery struct {
	Note      string             `json:"note"`
	Decisions []RecoveryDecision `json:"decisions"`
}

type RecoveryDecision struct {
	StepID string          `json:"step_id"`
	Action string          `json:"action"`
	Output json.RawMessage `json:"output,omitempty"`
}

// RecoveryRecord retains the operator's explanation and output digests. Outputs
// themselves stay on the resolved invocation, rather than being copied into history.
type RecoveryRecord struct {
	At        time.Time            `json:"at"`
	Note      string               `json:"note"`
	Decisions []RecoveryResolution `json:"decisions"`
}

type RecoveryResolution struct {
	StepID       string `json:"step_id"`
	Action       string `json:"action"`
	OutputSHA256 string `json:"output_sha256,omitempty"`
}

func UncertainSteps(r Run) []ScopedStep {
	var uncertain []ScopedStep
	for _, step := range ScopedSteps(r) {
		container := step.Definition.Child != nil || len(step.Definition.ForEach) > 0
		wait := step.Definition.Tool == "workflow_wait" || step.Definition.Tool == "workflow_wait_callback"
		if step.Result.Status == "running" && !container && !wait {
			uncertain = append(uncertain, step)
		}
	}
	return uncertain
}

func (r Run) Paused() bool {
	return r.Status == "waiting_review" || r.Status == "waiting"
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
			if step.Iterations != nil {
				definition := definitions[step.ID]
				definition.ForEach, definition.Concurrency, definition.OnError, definition.Review = nil, 0, "", false
				definition.Repeat = nil
				for _, item := range step.Iterations {
					definition.Args = item.Args
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
