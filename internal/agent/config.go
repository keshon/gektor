package agent

import (
	"context"
	"encoding/json"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/permission"
)

// Config wires everything one Agent run needs.
type Config struct {
	// BeginChanges starts a per-run observer independent of tool names.
	BeginChanges func(context.Context) (func() ([]string, error), error)
	Client       llm.Client
	Tools        *Registry
	System       string

	// MaxSteps bounds model round-trips per run. Zero: DefaultMaxSteps.
	MaxSteps int

	// TodoFunding extends the step budget for acknowledged checklist work.
	// Direct runs set it; bounded workers use fixed budgets by construction.
	TodoFunding bool

	// MaxTokens caps generation per response. Zero: 8192, because too
	// little truncates a large write mid-JSON, which reads as a model
	// failure rather than a missing request parameter.
	MaxTokens   int
	Temperature float64

	// ContextLimit is the backend's real window, e.g. from
	// MaxContextLength. Zero disables budget tracking: there is no
	// model-agnostic default worth guessing.
	ContextLimit int

	// MaxStuckSteps tolerates consecutive unproductive steps before a
	// nudge. Unproductive means every call errored, or the calls exactly
	// repeat — a tool that keeps returning the same listing is stuck too.
	// Zero: 2.
	MaxStuckSteps int

	// MaxExploratorySteps tolerates consecutive mutating-free steps before
	// suggesting a broader search. Distinct from MaxStuckSteps: the model
	// can look productive by this loop's definition while narrowing the
	// same dead end. Zero: 8; above MaxSteps disables it.
	MaxExploratorySteps int

	// SkipVerify drops the self-check round. Leave false unless the extra
	// round-trip is not worth it: a model can "save" by writing an empty
	// file, which this catches.
	SkipVerify bool

	// VerifyOnZeroWrites declares this run should produce files, so a
	// finish with no mutating call is refused rather than answered. The
	// self-check round still runs despite SkipVerify. Leave false where
	// writing nothing is a valid outcome.
	VerifyOnZeroWrites bool

	// Observers. All optional; the loop itself never prints.
	OnStep       func(step int, msg llm.Message)
	OnUsage      func(step int, usage llm.Usage)
	OnToolResult func(callID, result string)
	OnResult     func(ToolResult)
	OnDelta      func(chunk string)
	// OnNudge receives harness text (verify rounds, refusals, wrap-ups)
	// with a kind tag.
	OnNudge func(kind, text string)

	// StateFile gets the full history written after every step, so a
	// crash or MaxSteps resume loses at most one step.
	StateFile string

	// MutatingTools names the tools that change the filesystem, so the
	// self-check can state a fact instead of trusting the model's claim
	// that it saved something. Zero: this project's file tools.
	MutatingTools []string

	// Verify runs once at the self-check checkpoint — a real build or
	// test — and folds its output in as ground truth.
	Verify func(ctx context.Context) (output string, ok bool)

	// CompactKeepSteps is how many recent step groups compaction keeps.
	// Zero: DefaultCompactKeepSteps. -1 disables compaction.
	CompactKeepSteps int

	// CompactAtPercent is the share of ContextLimit that triggers
	// compaction. Zero: defaultCompactAtPercent.
	//
	// The window is not the model's working range: a local model holds a
	// plan across far less context than it will accept, so compacting near
	// full means every step before that already ran degraded. A run once
	// reached 57k of 65k, collapsed into one repeated token, then
	// described the truncation notice as if it were the task. The right
	// value is a property of the model, hence configuration.
	CompactAtPercent int

	// ReasoningBudget caps <think> deliberation per response, in
	// characters (no backend reports reasoning tokens separately).
	// Exceeding it earns one wrap-up round demanding commitment. Zero:
	// defaultReasoningBudget. Negative disables wrapping.
	ReasoningBudget int

	// MaxThinkWraps bounds wrap-up rounds per run; after that the step
	// budget ends the run. Zero: defaultMaxThinkWraps.
	MaxThinkWraps int

	// Policy gates tool calls. Zero value allows all.
	Policy permission.Policy

	// BeforeToolCall returns the policy effect plus the resource used for
	// matching (command, path, URL). Ask means prompt; the hook performs
	// the prompt and returns an error to block. Nil means no prompting, so
	// Ask degrades to Deny.
	BeforeToolCall func(ctx context.Context, tool, resource string, args json.RawMessage) (permission.Effect, error)

	// AfterToolCall observes every completed call.
	AfterToolCall func(tool, resource, result string, err error)

	// Stream uses SSE with OnDelta per chunk. Effective on backends with
	// SSE support; koboldcpp falls back to a unary call.
	Stream bool
}

// DefaultMaxSteps bounds a run that names no budget. Shared with the TUI's
// step meter so the displayed limit is the enforced one.
const DefaultMaxSteps = 25

// DefaultCompactKeepSteps is how many step groups compaction keeps.
const DefaultCompactKeepSteps = 8

func New(cfg Config) *Agent {
	if cfg.MaxSteps == 0 {
		cfg.MaxSteps = DefaultMaxSteps
	}
	if cfg.MaxStuckSteps == 0 {
		cfg.MaxStuckSteps = 2
	}
	if cfg.MaxExploratorySteps == 0 {
		cfg.MaxExploratorySteps = 8
	}
	if cfg.MaxTokens == 0 {
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
	if cfg.ReasoningBudget == 0 {
		cfg.ReasoningBudget = defaultReasoningBudget
	}
	if cfg.MaxThinkWraps == 0 {
		cfg.MaxThinkWraps = defaultMaxThinkWraps
	}
	return &Agent{cfg: cfg}
}

// defaultReasoningBudget caps deliberation per response: roughly 1500
// tokens at chars/4. Qwen 3.8 used 119-502 chars on trivial tasks, so 6000
// leaves ~12x headroom — a real ramble trips it, normal reasoning never
// comes close. Negative disables wrapping.
const defaultReasoningBudget = 6000

// defaultMaxThinkWraps bounds wrap-up rounds per run.
const defaultMaxThinkWraps = 2