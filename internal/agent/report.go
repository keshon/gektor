package agent

import (
	"github.com/keshon/gektor/internal/llm"
)

// MutatedPaths returns the workspace paths mutated by the last run,
// for session-end sweeps. Empty when nothing mutated.
func (a *Agent) MutatedPaths() []string {
	return a.report.MutatedPaths
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
	Summary      string
	Outcome      string
	Verification string
	Warnings     []string
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

// Report returns measured facts about the most recent Run/Resume,
// including a partially filled report for a run that errored mid-way.
func (a *Agent) Report() RunReport {
	return a.report
}
