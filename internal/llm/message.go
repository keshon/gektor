package llm

import "encoding/json"

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
	// Rejected input is archived as text, never replayed as a tool call.
	RawArguments string `json:"raw_arguments,omitempty"`
	InputError   string `json:"input_error,omitempty"`
}

// Message is one turn. ToolCalls is set only on assistant messages that
// invoke tools; ToolCallID only on tool results answering a call.
type Message struct {
	// ProviderState retains opaque reasoning between Responses tool turns.
	// It is saved with the assistant message, never sent to another backend.
	ProviderState *ReplayState `json:"provider_state,omitempty"`
	// Harness marks model-facing diagnostics. Delivery is recorded only when
	// the message is included in a request, independently of UI visibility.
	Harness string `json:"harness,omitempty"`
	Request int    `json:"request,omitempty"`
	// ToolFailed is durable display metadata; wire encoders omit it.
	ToolFailed   *bool  `json:"tool_failed,omitempty"`
	ToolStatus   string `json:"tool_status,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
	Role         Role
	Content      string
	ToolCalls    []ToolCall
	ToolCallID   string

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

// ModelMessages omits rejected calls without inventing empty arguments.
// Their paired harness diagnostics remain ordinary, valid messages.
func ModelMessages(history []Message) []Message {
	out := make([]Message, 0, len(history))
	for _, m := range history {
		m.ToolCalls = append([]ToolCall(nil), m.ToolCalls...)
		calls := m.ToolCalls[:0]
		for _, c := range m.ToolCalls {
			if c.InputError == "" {
				calls = append(calls, c)
			}
		}
		m.ToolCalls = calls
		m.Request = 0 // delivery metadata is never part of a model request
		if m.Role == RoleAssistant && len(calls) == 0 && m.Content == "" && len(m.Images) == 0 && m.ProviderState == nil {
			continue
		}
		out = append(out, m)
	}
	return out
}
