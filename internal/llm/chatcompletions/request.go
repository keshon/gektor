package chatcompletions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

func (c *Client) request(ctx context.Context, req llm.ChatRequest, stream bool) (*http.Request, error) {
	// Precedence: a per-request grammar (the structured plan and verdict
	// calls, which are schema constraints and correct on either backend)
	// wins, then an explicit client override, then whatever this backend
	// wants for ordinary responses.
	structured := req.Grammar != "" || req.JSONSchema != ""
	grammar := c.Grammar
	if grammar == "" && !c.NoGrammar {
		grammar = c.dialect.contentGrammar()
	}
	temperature := req.Temperature
	if temperature <= 0 {
		temperature = defaultTemperature
	}
	wreq := wireRequest{
		Model:       c.model,
		MaxTokens:   req.MaxTokens,
		Temperature: temperature,
		TopP:        defaultTopP,
		TopK:        defaultTopK,
	}
	if structured {
		c.dialect.applyStructured(&wreq, req.Grammar, req.JSONSchema)
	} else if grammar != "" {
		c.dialect.applyStructured(&wreq, grammar, "")
	}
	c.dialect.applySampling(&wreq, c.DRY)

	// koboldcpp serves the same endpoint shape but cannot process
	// pictures: attaching there fails here, loudly, instead of sending
	// a request the backend silently misunderstands.
	_, kobold := c.dialect.(koboldDialect)
	msgs, err := c.encodeRequestMessages(req.Messages, !kobold, req.UserRequest, req.UserRequestCount, req.RunDirective)
	if err != nil {
		return nil, err
	}
	wreq.Messages = msgs

	for _, t := range req.Tools {
		wt := wireTool{Type: "function"}
		wt.Function.Name = t.Name
		wt.Function.Description = t.Description
		wt.Function.Parameters = t.Parameters
		wreq.Tools = append(wreq.Tools, wt)
	}

	if stream {
		wreq.Stream, wreq.StreamOptions = true, &streamOptions{IncludeUsage: true}
		if _, ok := c.dialect.(llamaDialect); ok {
			wreq.ReturnProgress, wreq.TimingsPerToken = true, true
		}
	}
	body, err := json.Marshal(wreq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	c.logDebug("[%s] --- request ---\n%s\n\n", time.Now().Format(time.RFC3339), body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.chatURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	for k, v := range c.ExtraHeaders {
		if k != "" && v != "" {
			httpReq.Header.Set(k, v)
		}
	}

	return httpReq, nil
}

func responseError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	snippet := string(raw)
	if len(snippet) > 500 {
		snippet = snippet[:500] + "...(truncated)"
	}
	api := &llm.APIError{Status: resp.StatusCode, StatusText: resp.Status, Body: snippet}
	if v := resp.Header.Get("Retry-After"); v != "" {
		api.RetryAfter = llm.ParseRetryAfter(v)
	}
	if v := resp.Header.Get("retry-after-ms"); v != "" {
		if d := llm.ParseRetryAfterMs(v); d > 0 {
			api.RetryAfter = d
		}
	}
	if resp.StatusCode == http.StatusBadRequest && llm.IsOverflow(fmt.Errorf("%s", snippet)) {
		api.Overflow = true
	}
	return api
}
