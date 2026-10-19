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
	return &Agent{cfg: cfg}
}
