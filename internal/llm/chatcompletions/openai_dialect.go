// Remote OpenAI-compatible backend.
//
// One dialect covers every hosted provider that speaks the OpenAI chat
// format: OpenAI itself, OpenRouter, DeepSeek, Groq, Together, Mistral,
// xAI, Ollama/LM Studio reached over a network, and similar gateways.
// The wire shape is already what Server sends; what differs from the
// local dialects is what must NOT be sent: no GBNF grammar, no
// koboldcpp/llama-server sampler spellings, no top_k (not an OpenAI
// field). Authentication is a bearer token on every request.
package chatcompletions

import "encoding/json"

// NewOpenAIClient talks to any OpenAI-compatible /chat/completions endpoint.
// baseURL is the API root with or without a trailing /v1, e.g.
// "https://api.openai.com/v1" or "https://openrouter.ai/api/v1".
// Use WithAPIKey to attach the bearer token.
func NewOpenAIClient(baseURL, model string) *Server {
	return newServer(baseURL, model, openaiDialect{})
}

type openaiDialect struct{}

func (openaiDialect) name() string { return "openai-compatible" }

// Remote providers constrain tool calls from the tool schemas themselves.
// Sending the local content guard would constrain nothing they understand
// and risks fighting the provider's own template.
func (openaiDialect) contentGrammar() string { return "" }

// Structured output on the chat endpoint is response_format. With a schema
// the strict json_schema form names the schema so the provider validates
// against it; without one json_object still forces JSON rather than prose.
func (openaiDialect) applyStructured(req *wireRequest, gbnf, schema string) {
	if schema == "" {
		req.ResponseFormat = &responseFormat{Type: "json_object"}
		return
	}
	req.ResponseFormat = &responseFormat{
		Type: "json_schema",
		JSONSchema: &jsonSchemaDef{
			Name:   "response",
			Strict: true,
			Schema: json.RawMessage(schema),
		},
	}
}

// The shared Chat path sets the local sampler defaults before this runs.
// OpenAI-compatible APIs take temperature and top_p; top_k, rep_pen and
// the DRY fields are local inventions a hosted API rejects or ignores,
// so clear them here rather than teaching Chat about every dialect.
func (openaiDialect) applySampling(req *wireRequest, dry bool) {
	req.TopK = 0
	req.RepPen = 0
	req.RepPenRange = 0
	req.RepeatPenalty = 0
	req.RepeatLastN = 0
	req.DRYMult = 0
	req.DRYBase = 0
	req.DRYAllowed = 0
}
