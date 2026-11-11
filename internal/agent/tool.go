// Package agent runs model/tool conversations with bounded execution, context
// management, and archived history. Callers supply the model client, tools,
// permissions, and observers; concrete providers and tool implementations live
// outside this package.
package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

// ToolResult carries execution status separately from model-facing prose.
type ToolResult struct {
	Duration time.Duration
	CallID   string
	Output   string
	Failed   bool
	Status   string
}

// ToolMode tells the loop whether a tool may run concurrently with others
// in the same step, or must run alone. A type-level signal cannot know
// whether two write_file calls touch the same path, so rather than inspect
// arguments, every Exclusive tool just runs alone — occasionally
// serializing two writes to different files is cheaper than a write racing
// a read of the same path.
type ToolMode int

const (
	// Concurrent tools are safe to run alongside anything else in the
	// same batch: read-only operations.
	Concurrent ToolMode = iota
	// Exclusive tools mutate the workspace, or do something unanalyzable
	// (an arbitrary shell command). They separate concurrent read batches
	// and execute in model call order.
	Exclusive
)

func (m ToolMode) String() string {
	switch m {
	case Concurrent:
		return "Concurrent"
	case Exclusive:
		return "Exclusive"
	default:
		return "unknown"
	}
}

// Tool is anything the agent can call by name with JSON arguments.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage // JSON schema for the arguments object
	Mode() ToolMode
	Run(ctx context.Context, args json.RawMessage) (string, error)
}

// Attacher is implemented by a tool whose successful result includes
// pictures the model should see. The loop asks for absolute paths after
// Run succeeds and appends them to a user turn, because the OpenAI wire
// format has no image parts in a role:tool message. A tool without this
// interface is exactly as before.
type Attacher interface {
	Attach(args json.RawMessage) ([]string, error)
}

// ContextReuser may shorten a successful, already limited result using only
// complete content in the outgoing projection. It must validate completeness.
// Stored history stays intact; the projection is rebuilt after compaction.
type ContextReuser interface {
	ReuseInContext(args json.RawMessage, result string, history []llm.Message) string
}

// Checklister is a tool carrying the model's checklist, which the loop
// reads for reporting and restores on resume. *tools.Todo is one. The
// interface exists so the loop finds the checklist by shape rather than
// by the name "todo".
type Checklister interface {
	Tool
	Progress() (done int, open []string)
}

// Questioner is a tool that puts a question to a human. The loop finds an
// unanswered one in history on resume, so it needs the question text back
// out of the call arguments.
type Questioner interface {
	Tool
	Question(args json.RawMessage) string
}

// ToolCallID identifies the currently executing call without adding model arguments.
type toolCallIDKey struct{}

func ToolCallID(ctx context.Context) string { id, _ := ctx.Value(toolCallIDKey{}).(string); return id }
