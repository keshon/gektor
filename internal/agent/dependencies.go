package agent

import (
	"context"
	"encoding/json"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
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
