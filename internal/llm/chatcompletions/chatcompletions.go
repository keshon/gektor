// Package chatcompletions implements Chat Completions requests, responses and
// streaming. Request dialects adapt grammar and sampling fields; provider
// inspection and model discovery are composed separately by the application.
package chatcompletions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

// Client holds one route's configuration and usage totals. Set launch options
// before making requests; usage counters are synchronized separately.
type Client struct {
	baseURL string
	model   string
	dialect dialect
	http    *http.Client

	// Debug, if set, receives the raw JSON request and response bodies
	// for every call — the fastest way to find out whether something like
	// koboldcpp's own "[TOOLCALL REASONING]" console output actually rides
	// along in the HTTP response, or only ever exists in the server's own
	// terminal.
	Debug io.Writer

	// DRY enables the DRY sampler on every request. Off by default; see
	// the sampling defaults for why.
	DRY bool

	// NoGrammar disables the backend's own content grammar. Set it to
	// find out whether that grammar is helping or getting in the way;
	// per-request structured grammars are unaffected.
	NoGrammar bool

	// Grammar, if set, is sent as a GBNF constraint on every response.
	// koboldcpp already grammar-constrains the arguments of a tool call it
	// decides to make; it does NOT constrain the plain-text path taken
	// when it decides not to call one — which is exactly where a model's
	// own native tool-call template tokens (e.g. "<|tool_call>...") can
	// leak into what should be a normal answer. See DefaultGrammar.
	Grammar string

	// APIKey, when set, is sent as a bearer token on every request. Local
	// servers ignore it; hosted OpenAI-compatible APIs require it. Prefer
	// WithAPIKey or APIKeyFromEnv over embedding a secret in a flag.
	APIKey string

	// ExtraHeaders are sent on every chat request alongside auth, e.g.
	// the referrer headers OpenRouter asks for. Empty by default.
	ExtraHeaders map[string]string

	mu          sync.Mutex // guards usage totals
	totalPrompt int
	totalGen    int
	totalCalls  int
	totalCached int
}

// ClientFor builds a wire client using the selected request dialect.
func ClientFor(kind, baseURL, model string) (*Client, error) {
	for _, k := range kinds {
		if k.kind == kind {
			return newClient(baseURL, model, k.dialect), nil
		}
	}

	return nil, fmt.Errorf("unknown backend kind %q, want one of %s",
		kind, strings.Join(Kinds(), " or "))
}

// UsageTotals returns cumulative tokens across all Chat/Stream calls.
func (c *Client) UsageTotals() (prompt, generated, calls int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.totalPrompt, c.totalGen, c.totalCalls
}

// CachedTotal returns cumulative prefix-cached prompt tokens. Zero on
// backends that do not report them.
func (c *Client) CachedTotal() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.totalCached
}

func (c *Client) recordUsage(promptTokens, completionTokens, cachedTokens int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalPrompt += promptTokens
	c.totalGen += completionTokens
	c.totalCalls++
	c.totalCached += cachedTokens
}

func newClient(baseURL, model string, d dialect) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		dialect: d,
		// Long: a full context on a local model can take minutes to
		// process before a single token is generated.
		http: &http.Client{Timeout: 120 * time.Minute},
	}
}

// Backend names the server this client talks to, for recorded results.
func (c *Client) Backend() string { return c.dialect.name() }

// WithAPIKey attaches a bearer token to every chat request and returns
// the same client for chaining.
func (c *Client) WithAPIKey(key string) *Client {
	c.APIKey = key
	return c
}

// chatURL joins the configured base with the completions path. Local
// servers are configured as a bare origin ("http://localhost:5001") while
// hosted APIs hand out a root that already ends in /v1, so accept both
// plus a fully qualified path for gateways with deeper prefixes.
func (c *Client) chatURL() string {
	base := strings.TrimRight(c.baseURL, "/")
	if len(base) >= len("/chat/completions") && base[len(base)-len("/chat/completions"):] == "/chat/completions" {
		return base
	}

	if len(base) >= 3 && base[len(base)-3:] == "/v1" {
		return base + "/chat/completions"
	}

	return base + "/v1/chat/completions"
}

// debugSyncer lets logDebug flush *os.File-backed Debug writers
// immediately, so agent-debug.log is readable while the agent is still
// running instead of only after it exits.
type debugSyncer interface{ Sync() error }

func (c *Client) logDebug(format string, args ...any) {
	if c.Debug == nil {
		return
	}

	fmt.Fprintf(c.Debug, format, args...)
	if f, ok := c.Debug.(debugSyncer); ok {
		_ = f.Sync()
	}
}

func (c *Client) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	httpReq, err := c.request(ctx, req, false)
	if err != nil {
		return llm.ChatResponse{}, err
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("call backend: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return llm.ChatResponse{}, responseError(resp)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("read response body: %w", err)
	}

	c.logDebug("[%s] --- response (status %s) ---\n%s\n\n", time.Now().Format(time.RFC3339), resp.Status, bodyBytes)

	var wresp wireResponse
	if err := json.Unmarshal(bodyBytes, &wresp); err != nil {
		snippet := string(bodyBytes)
		if len(snippet) > 500 {
			snippet = snippet[:500] + "...(truncated)"
		}

		return llm.ChatResponse{}, fmt.Errorf("decode response: %w — backend said: %s", err, snippet)
	}

	if len(wresp.Choices) == 0 {
		return llm.ChatResponse{}, fmt.Errorf("backend returned no choices")
	}

	wm := wresp.Choices[0].Message
	out := llm.Message{
		Role:      llm.Role(wm.Role),
		Content:   decodeContent(wm.Content),
		Reasoning: wm.Reasoning,
	}

	for _, tc := range wm.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, llm.ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			// Arguments reach tools as the backend sent them, including a
			// bare string or invalid bytes, so the tool's own schema error
			// is what the model sees. Wrapping them here would hide it.
			Arguments: llm.NormalizeArguments(tc.Function.Arguments),
		})
	}

	usage := llm.Usage{
		PromptTokens:     wresp.Usage.PromptTokens,
		CompletionTokens: wresp.Usage.CompletionTokens,
		CachedTokens:     wresp.Usage.PromptTokensDetails.CachedTokens,
	}
	if usage.PromptTokens == 0 {
		usage.PromptTokens = llm.EstimatePromptTokens(req)
		usage.Estimated = true
	}

	c.recordUsage(usage.PromptTokens, usage.CompletionTokens, usage.CachedTokens)
	p, g, n := c.UsageTotals()
	c.logDebug("[%s] --- usage (this call: %d prompt (%d cached) + %d generated; run totals: %d prompt + %d generated over %d calls) ---\n\n",
		time.Now().Format(time.RFC3339), usage.PromptTokens, usage.CachedTokens, usage.CompletionTokens, p, g, n)

	return llm.ChatResponse{
		Message:      out,
		FinishReason: wresp.Choices[0].FinishReason,
		Usage:        usage,
	}, nil
}

// normalizeArguments handles the real shape of this wire format: per spec,
// function.arguments is a JSON-encoded *string*, e.g. the bytes
// `"{\"command\": \"ls\"}"` rather than `{"command": "ls"}` directly. Some
// local servers send the object form anyway, so accept either: if the raw
// bytes are themselves a JSON string, unwrap it once; otherwise pass
// through unchanged.
// encodeArguments is normalizeArguments' mirror image for outgoing
// requests: our internal ToolCall.Arguments is always the plain object
// form, so re-wrap it as a JSON string before it goes on the wire,
// matching what this backend actually expects.
func encodeArguments(args json.RawMessage) json.RawMessage {
	wrapped, err := json.Marshal(string(args))
	if err != nil {
		return json.RawMessage(`"{}"`)
	}

	return json.RawMessage(wrapped)
}

// WithHTTPClient installs the application-owned connection transport.
func (c *Client) WithHTTPClient(client *http.Client) { c.http = client }
