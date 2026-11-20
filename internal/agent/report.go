package agent

import (
	"slices"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

// MutatedPaths returns the workspace paths mutated by the last run,
// for session-end sweeps. Empty when nothing mutated.
func (a *Agent) MutatedPaths() []string {
	return slices.Clone(a.report.MutatedPaths)
}

// RunReport records the outcome, observed effects, and real check output.
type RunReport struct {
	// RecentTools keeps bounded references to recorded results, never model claims.
	ToolsExecuted int
	RecentTools   []ToolEvidence
	Usage         llm.Usage
	Archive       string
	Prompt        llm.ContextReport
	// Summary describes incomplete work; it never represents successful completion.
	Summary             string
	Outcome             string
	Verification        string
	VerificationReceipt *conversation.VerificationReceipt
	Warnings            []string
	// Steps is how many model round-trips the run performed.
	Steps int

	// MutatedPaths includes changes made by file tools, shells, MCP, and delegates.
	// Without an observer, these are paths from successful mutating calls.
	MutatedPaths []string

	// Final is the model's final answer text ("" if the run errored out).
	Final string

	// MaxSteps is the budget enforced this run — fixed operator limit —
	// so observers read the enforced line, not the configured one.
	MaxSteps int

	// OpenTodos is the checklist still unchecked when the run ended.
	OpenTodos []string

	// LastPromptTokens is the backend-reported prompt size of the last
	// completed call — how full the context actually got.
	LastPromptTokens int
}

type ToolEvidence struct {
	CallID string
	Name   string
	Status string
}

// Report returns a snapshot of measured facts about the most recent Run/Resume,
// including a partially filled report for a run that errored mid-way.
func (a *Agent) Report() RunReport {
	return a.report.Clone()
}

// Clone returns an independent snapshot for callers and event observers.
func (r RunReport) Clone() RunReport {
	r.RecentTools = slices.Clone(r.RecentTools)
	r.Warnings = slices.Clone(r.Warnings)
	r.MutatedPaths = slices.Clone(r.MutatedPaths)
	r.OpenTodos = slices.Clone(r.OpenTodos)
	r.Prompt.Sections = slices.Clone(r.Prompt.Sections)
	r.Prompt.Tools = slices.Clone(r.Prompt.Tools)
	r.Prompt.Notes = slices.Clone(r.Prompt.Notes)
	if r.VerificationReceipt != nil {
		receipt := *r.VerificationReceipt
		if receipt.ExitCode != nil {
			code := *receipt.ExitCode
			receipt.ExitCode = &code
		}
		r.VerificationReceipt = &receipt
	}
	return r
}
