// Package llm is the wire-level contract between the agent loop and a
// chat-completion backend. Backend-specific detail (quirky JSON, missing
// fields, weak-model workarounds) lives in a concrete Client, never here
// and never in the loop.
package llm

import "context"

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

type ChatRequest struct {
	// UserRequest is host attribution metadata, not a new conversation turn.
	// Local backends may repeat a bounded excerpt in a transient wire footer.
	UserRequest      string
	UserRequestCount int
	RunDirective     string

	// OnProgress observes this request without changing its wire payload.
	OnProgress func(Progress)
	Messages   []Message
	Tools      []ToolDef

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

// Progress separates server-reported prompt processing from generated output.
// Estimated counters are display-only; final Usage remains authoritative.
type Progress struct {
	HTTPStatus                                                    int
	Request                                                       int
	Attempt                                                       int
	RequestID                                                     string
	DurationMS                                                    int64
	Outcome                                                       string
	CacheKnown                                                    bool // Distinguishes a measured zero from unavailable cache telemetry.
	Phase                                                         string
	PromptTokens, CompletionTokens, CachedTokens, ProcessedTokens int
	Estimated                                                     bool
	Detail                                                        string
	Subphase                                                      string
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
