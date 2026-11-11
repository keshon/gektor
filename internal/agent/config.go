package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
)

// ChangeObserver captures a run baseline and reports its measured file effects.
// Begin is called per run; Observe must support finalization after cancellation.
// Implementations own their scan state and are used sequentially.
type ChangeObserver interface {
	Begin(context.Context) error
	Observe(context.Context) ([]string, error)
}

// ToolCatalog owns dynamic tool activation and its persisted capability state.
// PreviewDefs must not perform IO or mutate the registry. Restore precedes Prepare.
// A catalog belongs to one agent and is used sequentially.
type ToolCatalog interface {
	Prepare(context.Context) error
	PreviewDefs() []llm.ToolDef
	Save() []conversation.Capability
	Restore(context.Context, []conversation.Capability) error
}

// BrowserSelection owns the selected browser profile across saved runs.
// SelectProfile restores selection without activating browser tools.
type BrowserSelection interface {
	Reference() *conversation.BrowserProfile
	SelectProfile(*conversation.BrowserProfile) error
}

// ToolPaths resolves workspace instructions and canonical permission resources.
// Tool-specific resources take precedence over this workspace fallback. Empty
// resources use the ordinary command/URL; lookup errors prevent execution.
// Instruction lookup runs before admission and may withhold a mutation.
type ToolPaths interface {
	Instructions(json.RawMessage) (string, error)
	Resources(string, json.RawMessage) ([]string, error)
}

// OutputStore binds a run to retained tool output and bounds model-facing results.
// Context must preserve cancellation and existing values. Limit retains oversized
// output in the same store used by tools; it is called serially by Agent.
type OutputStore interface {
	Context(context.Context) context.Context
	Limit(string, int) string
}

// NotificationSource polls completed background work for the current tool context.
// Polling consumes pending notifications; Agent alone appends them to history.
type NotificationSource interface {
	Notifications(context.Context) []string
}

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

const DefaultMaxSteps = 25
const DefaultCompactKeepSteps = 8

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
