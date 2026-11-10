package retrospective

import (
	"fmt"
	"strings"
)

// Failure retains bounded review diagnostics, never a proposed lesson.
type Failure struct {
	Progress       Progress `json:"progress"`
	Cause          string   `json:"cause"`
	LastValidation string   `json:"last_validation,omitempty"`
	Recovery       string   `json:"recovery"`
	Path           string   `json:"path,omitempty"`
	err            error
}

func (f *Failure) Unwrap() error { return f.err }
func (f *Failure) Error() string {
	lines := []string{f.Cause, "Scope: " + f.Progress.Scope, f.Progress.Request,
		fmt.Sprintf("Completed %d/%d steps; inspected %d/%d messages (excerpts may be partial).", f.Progress.Step, f.Progress.Steps, f.Progress.Read, f.Progress.Total),
		"Last stage: " + f.Progress.Stage}
	if f.LastValidation != "" {
		lines = append(lines, "Last rejected submission: "+f.LastValidation)
	}
	lines = append(lines, f.Recovery)
	if f.Path != "" {
		lines = append(lines, "Diagnostics: "+f.Path, "Reopen with /learn review; no model call is needed.")
	}
	return strings.Join(lines, "\n")
}
