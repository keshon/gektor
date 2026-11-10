package service

import (
	"encoding/json"
	"time"

	"github.com/keshon/gektor/internal/workflow"
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
	ID           string           `json:"run_id"`
	Name         string           `json:"name"`
	RecipePath   string           `json:"recipe_path"`
	Status       string           `json:"status"`
	Started      time.Time        `json:"started"`
	Finished     *time.Time       `json:"finished,omitempty"`
	DurationMS   *int64           `json:"duration_ms,omitempty"`
	ScheduleID   string           `json:"schedule_id,omitempty"`
	ScheduledFor *time.Time       `json:"scheduled_for,omitempty"`
	Outputs      InspectionValue  `json:"outputs"`
	StepCounts   map[string]int   `json:"step_counts"`
	Errors       []StepInspection `json:"errors,omitempty"`
}

type StepInspection struct {
	ID        string `json:"id"`
	Operation string `json:"operation,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

func InspectSteps(r workflow.Run) []StepInspection {
	ops := make(map[string]string, len(r.Definition.Steps))
	for _, s := range r.Definition.Steps {
		ops[s.ID] = s.Tool
	}
	out := make([]StepInspection, 0, len(r.Steps))
	for _, s := range r.Steps {
		out = append(out, StepInspection{ID: s.ID, Operation: ops[s.ID], Status: s.Status, Error: s.Error})
	}
	return out
}

// InspectRun uses the store's observed status, which can differ from the last
// recorded engine status after a process exits unexpectedly.
func InspectRun(state store.RunState) RunInspection {
	r := state.Record
	out := RunInspection{ID: r.RunID, Name: r.Definition.Name, RecipePath: r.RecipePath, Status: state.Status,
		Started: r.Started, Finished: r.Finished, ScheduleID: r.ScheduleID, ScheduledFor: r.ScheduledFor,
		Outputs: InspectValue(DeclaredOutputs(r)), StepCounts: map[string]int{}}
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
