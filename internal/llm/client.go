// Package llm is the wire-level contract between the agent loop and a
// chat-completion backend. Backend-specific detail (quirky JSON, missing
// fields, weak-model workarounds) lives in a concrete Client, never here
// and never in the loop.
package llm

import (
	"context"
	"encoding/json"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall is a single function call the model wants to make.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// Message is one turn. ToolCalls is set only on assistant messages that
// invoke tools; ToolCallID only on tool results answering a call.
type Message struct {
	Role       Role
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string

	// Images attaches pictures as absolute paths, resolved at send time.
	// Paths rather than bytes keeps snapshots small; files are re-read per
	// request. Empty serializes to nothing.
	Images []string `json:"images,omitempty"`

	// Reasoning is deliberation a backend reports out-of-band (llama.cpp's
	// reasoning_content) instead of inline <think> tags. Measured by the
	// reasoning budget and kept in history, but never sent back: only some
	// templates accept it, and on a backend that passes extras through an
	// unknown field is a behaviour change, not a replay.
	Reasoning string
}

// ToolDef is what we tell the model a tool looks like.
type ToolDef struct {
	Name        string
	Description string
	Parameters  json.RawMessage // JSON schema for the arguments object
}

type ChatRequest struct {
	Messages []Message
	Tools    []ToolDef

	// MaxTokens caps generation. Left at the backend default, large
	// generations cut off mid-JSON — see agent.Config.MaxTokens.
	MaxTokens int

	// Grammar constrains this response with a GBNF grammar, overriding any
	// client default. For tool-free structured calls, where a weak model
	// asked for free-form JSON reliably drifts. Honoured only by backends
	// that take a grammar field; others ignore it.
	Grammar string

	// JSONSchema is the same constraint for backends that take structured
	// output this way. Supply both and let the dialect choose: a backend
	// silently ignoring the form it does not take once ran a whole mission
	// sweep unconstrained.
	JSONSchema string

	// Temperature, if > 0, is sent. Zero means "the client's documented
	// default", not "let the server decide" — a local backend silently
	// supplies its own for any omitted field, and a measurement taken
	// under unstated sampling is not comparable to the next one.
	//
	// There is deliberately no way to request a literal 0.0: greedy
	// sampling makes a grammar-constrained weak model loop on repeated
	// tokens. Structured calls pass a low value instead.
	Temperature float64
}

// Usage reports the backend's own tokenizer counts, not a tiktoken
// approximation, which would measure the wrong vocabulary for a local
// model. PromptTokens against agent.Config.ContextLimit says how full the
// context really is.
type Usage struct {
	PromptTokens     int
	CompletionTokens int

	// CachedTokens is how much of PromptTokens came from prefix cache
	// (llama.cpp reports it; koboldcpp reports nothing and leaves it
	// zero). Cached tokens still occupy the window, so budgets use the
	// full PromptTokens; this split is for accounting only.
	CachedTokens int

	// Estimated marks a chars/4 fallback: some backends report no usage
	// block, and a dead meter is worse than an honest estimate. Displays
	// with a "~"; budgets treat estimates as measurements.
	Estimated bool
}

type ChatResponse struct {
	Message Message
	Usage   Usage

	// FinishReason is the backend's own reason generation ended: "stop",
	// "length", "tool_calls", or "" if it didn't say. "length" means an
	// incomplete stump with any in-flight tool call discarded, so the loop
	// must never treat it as a deliberate finish — live shape: a backend
	// cut a worker off after 38 tokens of preamble and the loop accepted
	// "Let me write the file…" as the answer.
	FinishReason string
}

// Client talks to exactly one backend. Implementations own everything
// specific to it: prompt formatting, tool-call JSON quirks, repairing
// malformed output. The loop never knows which backend it is talking to.
type Client interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

// Streamer is implemented by backends that can deliver a turn
// incrementally. Reasoning and content arrive as separate callbacks rather
// than one merged stream, because a thinking model reports deliberation
// first and a reader wants to watch it happen before the answer starts.
//
// It used to be an anonymous interface asserted at the call site, with
// capability negotiated by matching an error string; naming it means a
// backend either streams or it does not, visibly.
type Streamer interface {
	Stream(ctx context.Context, req ChatRequest, onReasoning, onContent func(string)) (ChatResponse, error)
}