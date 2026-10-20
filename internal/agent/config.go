package agent

import (
	"context"
	"encoding/json"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/permission"
)

// Config supplies capabilities, limits and observers for one run.
type Config struct {
	BeginChanges  func(context.Context) (func() ([]string, error), error)
	Client        llm.Client
	Tools         *Registry
	System        string
	MaxSteps      int
	MaxTokens     int
	Temperature   float64
	ContextLimit  int
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
	// Directive is the harness turn that states this role's stance, and
	// Supersedes is the stance turn it answers. A stance cannot live in
	// System: System is the prompt's first message, so changing it when the
	// role changes throws away the cached prefix. The loop keeps a stance
	// turn at the end of the conversation instead, appending one only when
	// the conversation ended in a different stance. Empty Directive means
	// the role has no stance turn at all.
	Directive  string
	Supersedes string
	// Verify runs the configured command once after the final answer.
	// Its output is recorded without another model call.
	Verify           func(context.Context) (output string, err error)
	CompactKeepSteps int
	CompactAtPercent int
	Policy           permission.Policy
	BeforeToolCall   func(context.Context, string, string, json.RawMessage) (permission.Effect, error)
	ToolHint         func(string) string
	Stream           bool
	OnStep           func(int, llm.Message)
	OnUsage          func(int, llm.Usage)
	OnProgress       func(llm.Progress)
	OnResult         func(ToolResult)
	OnDelta          func(string)
	OnReasoning      func(string)
	OnNudge          func(kind, text string)
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
		cfg.MutatingTools = []string{"write_file", "patch_file", "patch_lines", "move_file"}
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
