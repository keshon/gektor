// Package agent executes model conversations and tool calls with bounded context
// and persisted checkpoints. Callers supply clients, tools, and execution policy.
package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
)

// Config supplies services, limits, and observers for an Agent's sequential runs.
type Config struct {
	// Changes owns a baseline per run and must not be shared by concurrent agents.
	Changes ChangeObserver
	Client  llm.Client
	Inbox   *Inbox
	OnInput func(llm.Message)
	Tools   *Registry
	// Catalog manages dynamic tools in Tools. Nil leaves a static registry.
	Catalog        ToolCatalog
	Browser        BrowserSelection
	System         string
	SystemSections []llm.PromptSection
	OnPrompt       func(llm.ContextReport)
	MaxSteps       int
	// RunTimeout includes work and a short tool-free closing phase. Zero disables it.
	RunTimeout    time.Duration
	MaxTokens     int
	Temperature   float64
	ContextLimit  int
	Workspace     string
	StateFile     string
	MutatingTools []string
	// ReadOnly names the only tools this role may run. The registry still
	// advertises the full set: a narrower one would change the prompt
	// prefix, and a backend that caches prompts would then re-read the whole
	// conversation whenever the operator switches roles. A call outside the
	// set is refused before it runs, so the capability is still removed by
	// the harness rather than by asking the model to hold back. Nil means
	// unrestricted; a non-nil empty slice means nothing may run.
	ReadOnly []string
	// AllowReadOnlyExtensions lets Planner admit locally declared read-only tools.
	// Other restricted roles retain the exact allowlist above.
	AllowReadOnlyExtensions bool
	// Directive is the harness turn that states this role's stance, and
	// Supersedes is the stance turn it answers. A stance cannot live in
	// System: System is the prompt's first message, so changing it when the
	// role changes throws away the cached prefix. The loop keeps a stance
	// turn at the end of the conversation instead, appending one only when
	// the conversation ended in a different stance. Empty Directive means
	// the role has no stance turn at all.
	Directive  string
	Supersedes string
	// Verify performs configured verification once after the final answer.
	// Its output is recorded without another model call.
	Verify           func(context.Context) (output string, err error)
	CompactKeepSteps int
	CompactAtPercent int
	Policy           permission.Policy
	BeforeToolCall   func(context.Context, string, string, json.RawMessage) (permission.Effect, error)
	Paths            ToolPaths
	ToolHint         func(string) string
	Stream           bool
	OnStep           func(int, llm.Message)
	OnUsage          func(int, llm.Usage)
	OnProgress       func(llm.Progress)
	OnResult         func(ToolResult)
	// OnToolFinished is a transient execution-complete signal. OnResult follows
	// after ordered attachment and history processing. Called serially.
	OnToolFinished func(callID string)
	// OnToolOutput receives transient preview chunks, never model history.
	OnToolOutput func(callID, text string)
	OnDelta      func(string)
	OnReasoning  func(string)
	OnNudge      func(kind, text string)
	OnContext    func(llm.Message)
	// ToolContext adds adapter metadata after output binding, before run deadlines.
	ToolContext   func(context.Context) context.Context
	Outputs       OutputStore
	Notifications NotificationSource
	Summarize     bool
	OnLocal       func(string)
}

// Agent owns the state of sequential runs and borrows the services in Config.
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

const DefaultMaxSteps = 25

const DefaultCompactKeepSteps = 8

// New applies execution defaults and retains cfg's service references.
// The caller owns those services; use one Agent for sequential runs.
func New(cfg Config) *Agent {
	if cfg.ToolHint == nil {
		cfg.ToolHint = hintResolver(cfg.Tools)
	}

	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = DefaultMaxSteps
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 8192
	}

	if len(cfg.MutatingTools) == 0 {
		cfg.MutatingTools = []string{"write_file", "patch_file", "move_file"}
	}

	if cfg.CompactKeepSteps == 0 {
		cfg.CompactKeepSteps = DefaultCompactKeepSteps
	}
	if cfg.CompactAtPercent == 0 {
		cfg.CompactAtPercent = defaultCompactAtPercent
	}

	a := &Agent{cfg: cfg}
	if cfg.ReadOnly != nil {
		a.allowed = make(map[string]bool, len(cfg.ReadOnly))
		for _, name := range cfg.ReadOnly {
			a.allowed[name] = true
		}
	}

	return a
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
		{
			Role:    llm.RoleUser,
			Content: task,
			Images:  images,
		},
	}
	// A new conversation can already have tool and browser selections.
	_, err := a.restoreHistory(nil)
	if err != nil {
		return "", err
	}
	a.snapshot.History = history
	a.checkpointLen = len(history)
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
		history = append(history, llm.Message{
			Role:    llm.RoleUser,
			Content: note,
			Images:  images,
		})
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
