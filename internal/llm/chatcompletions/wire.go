package chatcompletions

import (
	"encoding/json"
	"strings"
)

type wireFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type wireToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCalls  []wireToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	// Inbound only: llama.cpp reports a thinking model's deliberation
	// here instead of inline <think> tags. Decoded into Message but
	// never encoded back (see Message.Reasoning).
	Reasoning string `json:"reasoning_content,omitempty"`
}

type wireTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type wireRequest struct {
	ReturnProgress  bool            `json:"return_progress,omitempty"`
	TimingsPerToken bool            `json:"timings_per_token,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	StreamOptions   *streamOptions  `json:"stream_options,omitempty"`
	Model           string          `json:"model"`
	Messages        []wireMessage   `json:"messages"`
	Tools           []wireTool      `json:"tools,omitempty"`
	Grammar         string          `json:"grammar,omitempty"`
	ResponseFormat  *responseFormat `json:"response_format,omitempty"`
	MaxTokens       int             `json:"max_tokens,omitempty"`
	Temperature     float64         `json:"temperature,omitempty"`
	TopP            float64         `json:"top_p,omitempty"`
	TopK            int             `json:"top_k,omitempty"`

	// Repetition controls. Both backends pass unknown fields through to
	// the sampler, the same route the grammar field takes, and each
	// dialect fills only its own spelling — koboldcpp's rep_pen or
	// llama-server's repeat_penalty. The DRY names happen to agree.
	RepPen        float64 `json:"rep_pen,omitempty"`
	RepPenRange   int     `json:"rep_pen_range,omitempty"`
	RepeatPenalty float64 `json:"repeat_penalty,omitempty"`
	RepeatLastN   int     `json:"repeat_last_n,omitempty"`
	DRYMult       float64 `json:"dry_multiplier,omitempty"`
	DRYBase       float64 `json:"dry_base,omitempty"`
	DRYAllowed    int     `json:"dry_allowed_length,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type wireUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// decodeContent reads response content: servers answer with a plain
// string; a multipart array degrades to its text parts rather than
// failing the turn on an shape we never send outbound.
func decodeContent(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var parts []contentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder

		for _, p := range parts {
			if p.Type == "text" {
				b.WriteString(p.Text)
			}
		}

		return b.String()
	}

	return string(raw)
}

// responseFormat is the structured-output request carried on the chat
// endpoint. llama-server takes the schema inline; OpenAI takes the
// strict json_schema form. Each dialect fills only its own shape.
type responseFormat struct {
	Type       string          `json:"type"`
	Schema     json.RawMessage `json:"schema,omitempty"`
	JSONSchema *jsonSchemaDef  `json:"json_schema,omitempty"`
}

// jsonSchemaDef is OpenAI's named-schema wrapper for strict structured
// output.
type jsonSchemaDef struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
}

type wireResponse struct {
	Choices []struct {
		Message      wireMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage wireUsage `json:"usage"`
}
