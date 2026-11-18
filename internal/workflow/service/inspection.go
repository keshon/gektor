package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
	"github.com/keshon/gektor/internal/workflow/store"
)

// InspectionValue keeps projections valid JSON without copying large payloads.
// The complete value remains available in the stored run's full view.
type InspectionValue struct {
	Value   json.RawMessage `json:"value,omitempty"`
	Bytes   int             `json:"bytes"`
	Omitted bool            `json:"omitted,omitempty"`
}

func InspectValue(raw []byte) InspectionValue {
	v := InspectionValue{Bytes: len(raw)}
	if len(raw) > 8192 {
		v.Omitted = true
	} else if len(raw) != 0 {
		if json.Valid(raw) {
			v.Value = append(json.RawMessage(nil), raw...)
		} else {
			v.Value, _ = json.Marshal(string(raw))
		}
	}
	return v
}

type RunInspection struct {
	Uncertain    []StepInspection          `json:"uncertain,omitempty"`
	Recoveries   []workflow.RecoveryRecord `json:"recoveries,omitempty"`
	Callback     *workflow.CallbackState   `json:"callback,omitempty"`
	ResumeAt     *time.Time                `json:"resume_at,omitempty"`
	ID           string                    `json:"run_id"`
	Name         string                    `json:"name"`
	RecipePath   string                    `json:"recipe_path"`
	Status       string                    `json:"status"`
	Started      time.Time                 `json:"started"`
	Finished     *time.Time                `json:"finished,omitempty"`
	DurationMS   *int64                    `json:"duration_ms,omitempty"`
	ScheduleID   string                    `json:"schedule_id,omitempty"`
	ScheduledFor *time.Time                `json:"scheduled_for,omitempty"`
	Outputs      InspectionValue           `json:"outputs"`
	StepCounts   map[string]int            `json:"step_counts"`
	Errors       []StepInspection          `json:"errors,omitempty"`
}

type StepInspection struct {
	Attempts   int               `json:"attempt_count,omitempty"`
	Iterations int               `json:"iteration_count,omitempty"`
	Failure    *workflow.Failure `json:"failure,omitempty"`
	ID         string            `json:"id"`
	Operation  string            `json:"operation,omitempty"`
	Status     string            `json:"status"`
	Error      string            `json:"error,omitempty"`
}

func InspectSteps(r workflow.Run) []StepInspection {
	out := make([]StepInspection, 0, len(r.Steps))
	for _, s := range workflow.ScopedSteps(r) {
		out = append(out, StepInspection{Attempts: len(s.Result.Attempts), Iterations: len(s.Result.Iterations), ID: s.ID, Operation: s.Definition.Tool, Status: s.Result.Status, Error: s.Result.Error, Failure: s.Result.Failure})
	}
	return out
}

// InspectRun uses the store's observed status, which can differ from the last
// recorded engine status after a process exits unexpectedly.
func InspectRun(state store.RunState) RunInspection {
	r := state.Record
	out := RunInspection{Callback: workflow.PendingCallback(r), ResumeAt: r.WakeAt, ID: r.RunID, Name: r.Definition.Name, RecipePath: r.RecipePath, Status: state.Status,
		Started: r.Started, Finished: r.Finished, ScheduleID: r.ScheduleID, ScheduledFor: r.ScheduledFor,
		Outputs: InspectValue(DeclaredOutputs(r)), StepCounts: map[string]int{}}
	out.Recoveries = r.Recoveries
	if state.Status == "interrupted" {
		for _, step := range workflow.UncertainSteps(r) {
			out.Uncertain = append(out.Uncertain, StepInspection{ID: step.ID, Operation: step.Definition.Tool, Status: "uncertain"})
		}
	}
	if r.Finished != nil && !r.Started.IsZero() {
		ms := r.Finished.Sub(r.Started).Milliseconds()
		out.DurationMS = &ms
	}
	for _, s := range InspectSteps(r) {
		out.StepCounts[s.Status]++
		if s.Error != "" {
			out.Errors = append(out.Errors, s)
		}
	}
	return out
}

const defaultInvocationPage = 50
const maxInvocationPage = 100

// Inspection selects a read-only projection of a saved run. A nil Pointer
// selects the whole output; a pointer to an empty string explicitly selects its root.
type Inspection struct {
	View    string
	StepID  string
	Pointer *string
	Offset  int
	Limit   int
}

type InvocationPage struct {
	ID          string           `json:"run_id"`
	Status      string           `json:"status"`
	Invocations []StepInspection `json:"invocations"`
	Offset      int              `json:"offset"`
	Limit       int              `json:"limit"`
	Total       int              `json:"total"`
	NextOffset  int              `json:"next_offset,omitempty"`
}

type AttemptInspection struct {
	ID       string             `json:"run_id"`
	Step     StepInspection     `json:"step"`
	Attempts []workflow.Attempt `json:"attempts"`
}

type StepDetail struct {
	ID         string          `json:"run_id"`
	Step       StepInspection  `json:"step"`
	Args       InspectionValue `json:"args"`
	Output     InspectionValue `json:"output"`
	Attempts   int             `json:"attempt_count"`
	Iterations int             `json:"iteration_count"`
}

// InspectRecord borrows the saved record and never resumes or changes it.
// Invocation pages contain metadata only; use a scoped ID with step or attempts.
func InspectRecord(state store.RunState, selection Inspection) (any, error) {
	if selection.View == "" {
		selection.View = "summary"
	}
	requiresStep := selection.View == "step" || selection.View == "attempts"
	if requiresStep != (selection.StepID != "") {
		return nil, fmt.Errorf("step_id is required only for step or attempts view")
	}
	if selection.Pointer != nil && selection.View != "summary" && selection.View != "step" {
		return nil, fmt.Errorf("pointer requires summary or step view")
	}
	if selection.Offset < 0 || selection.Limit < 0 || selection.Limit > maxInvocationPage {
		return nil, fmt.Errorf("offset must be nonnegative and limit must be between 1 and 100 when supplied")
	}
	if selection.View != "invocations" && (selection.Offset != 0 || selection.Limit != 0) {
		return nil, fmt.Errorf("offset and limit require invocations view")
	}
	selectValue := func(raw []byte) (InspectionValue, error) {
		if selection.Pointer == nil {
			return InspectValue(raw), nil
		}
		value, err := recipe.OutputAt(raw, *selection.Pointer)
		if err != nil {
			return InspectionValue{}, err
		}
		return InspectValue(value), nil
	}

	switch selection.View {
	case "full":
		return state, nil
	case "summary":
		summary := InspectRun(state)
		if selection.Pointer != nil {
			output, err := selectValue(DeclaredOutputs(state.Record))
			if err != nil {
				return nil, err
			}
			summary.Outputs = output
		}
		return summary, nil
	case "steps":
		return struct {
			ID     string           `json:"run_id"`
			Status string           `json:"status"`
			Steps  []StepInspection `json:"steps"`
		}{ID: state.Record.RunID, Status: state.Status, Steps: InspectSteps(state.Record)}, nil
	case "invocations":
		limit := selection.Limit
		if limit == 0 {
			limit = defaultInvocationPage
		}
		steps := InspectSteps(state.Record)
		start := min(selection.Offset, len(steps))
		end := start + min(limit, len(steps)-start)
		page := InvocationPage{ID: state.Record.RunID, Status: state.Status, Invocations: steps[start:end], Offset: selection.Offset, Limit: limit, Total: len(steps)}
		if end < len(steps) {
			page.NextOffset = end
		}
		return page, nil
	case "step", "attempts":
		for _, scoped := range workflow.ScopedSteps(state.Record) {
			if scoped.ID != selection.StepID {
				continue
			}
			step := scoped.Result
			metadata := StepInspection{Attempts: len(step.Attempts), Iterations: len(step.Iterations), ID: scoped.ID, Operation: scoped.Definition.Tool, Status: step.Status, Error: step.Error, Failure: step.Failure}
			if selection.View == "attempts" {
				attempts := append([]workflow.Attempt{}, step.Attempts...)
				return AttemptInspection{ID: state.Record.RunID, Step: metadata, Attempts: attempts}, nil
			}
			output, err := selectValue([]byte(step.Output))
			if err != nil {
				return nil, err
			}
			return StepDetail{ID: state.Record.RunID, Step: metadata, Args: InspectValue(step.Args), Output: output, Attempts: len(step.Attempts), Iterations: len(step.Iterations)}, nil
		}
		return nil, fmt.Errorf("unknown step_id %q", selection.StepID)
	default:
		return nil, fmt.Errorf("unknown inspection view %q", selection.View)
	}
}
