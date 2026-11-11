package agent

import (
	"context"
	"strings"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

type Agent struct {
	cfg           Config
	snapshot      conversation.Snapshot
	checkpointLen int
	requests      int
	inspection    *ContextInspection

	// allowed is the read-only allowlist, nil when unrestricted.
	allowed map[string]bool

	// LastRunMutations counts changed paths. Embedders without
	// Changes fall back to successful mutating tool calls.
	LastRunMutations int

	report RunReport
}

// ToolNames returns the names of the tools this agent can call, sorted.
// Asserted on in tests: a harness that builds its agent by hand can
// otherwise ship a different tool set than the one it means to.
func (a *Agent) ToolNames() []string {
	if a.cfg.Tools == nil {
		return nil
	}
	defs := a.previewTools()
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	return names
}

// Run executes the agent loop on a single task and returns the model's
// final answer once it stops requesting tools. images attaches pictures
// (absolute paths) to the opening user message for vision-capable
// backends; imageless runs are unchanged.
func (a *Agent) Run(ctx context.Context, task string, images ...string) (string, error) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: a.cfg.System},
		{Role: llm.RoleUser, Content: task, Images: images},
	}
	a.snapshot, a.checkpointLen = conversation.New(history), len(history)
	return a.run(ctx, history)
}

// Resume continues from a previously saved history (see LoadState) after
// a run was interrupted — crashed, killed, or hit MaxSteps. note is
// appended as a fresh user message; the full history is already there, so
// it need not summarise what happened.
func (a *Agent) Resume(ctx context.Context, history []llm.Message, note string, images ...string) (string, error) {
	var err error
	history, err = a.restoreHistory(history)
	if err != nil {
		return "", err
	}
	history = a.closePendingCalls(a.resumeHistory(history), "", "")
	if strings.TrimSpace(note) != "" || len(images) > 0 {
		history = append(history, llm.Message{Role: llm.RoleUser, Content: note, Images: images})
	}
	return a.run(ctx, history)
}

// ResumeWithAnswer continues from a history that ended with an unanswered
// ask_user. answer becomes the matching role:tool message, not a user
// message: a backend expects every tool_call answered before anything
// else, and a naked user turn after one is wire-invalid.
func (a *Agent) ResumeWithAnswer(ctx context.Context, history []llm.Message, callID, answer string) (string, error) {
	var err error
	history, err = a.restoreHistory(history)
	if err != nil {
		return "", err
	}
	history = a.closePendingCalls(a.resumeHistory(history), callID, answer)
	return a.run(ctx, history)
}

// Tools returns the run's registry, so a caller holding an Agent can ask
// what it was offered — which question tool, whether a checklist exists.
func (a *Agent) Tools() *Registry { return a.cfg.Tools }
