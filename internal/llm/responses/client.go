// Package responses implements the ChatGPT OAuth Responses route.
package responses

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

type Client struct {
	reasoningEffort                                string
	reasoningSummary                               bool
	streamIdleTimeout                              time.Duration
	baseURL, model                                 string
	http                                           *http.Client
	tokenSource                                    func(context.Context) (string, error)
	replayScope                                    string
	mu                                             sync.Mutex
	totalPrompt, totalGen, totalCached, totalCalls int
}

func New(baseURL, model, scope string, token func(context.Context) (string, error)) *Client {
	return &Client{baseURL: baseURL, model: model, replayScope: scope, tokenSource: token, http: responseHTTPClient()}
}
func (c *Client) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	return c.responses(ctx, req, nil, nil)
}
func (c *Client) Stream(ctx context.Context, req llm.ChatRequest, r, t func(string)) (llm.ChatResponse, error) {
	return c.responses(ctx, req, r, t)
}
func (c *Client) recordUsage(p, g, cached int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalPrompt += p
	c.totalGen += g
	c.totalCached += cached
	c.totalCalls++
}
func (c *Client) UsageTotals() (int, int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalPrompt, c.totalGen, c.totalCalls
}
func (c *Client) CachedTotal() int { c.mu.Lock(); defer c.mu.Unlock(); return c.totalCached }

// ConfigureReasoning sets launch-scoped options before the client is used.
func (c *Client) ConfigureReasoning(effort string, summary bool) {
	c.reasoningEffort, c.reasoningSummary = effort, summary
}
