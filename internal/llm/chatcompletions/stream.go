package chatcompletions

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

// Stream sends req with stream:true and reports each chunk as it arrives,
// reasoning on onReasoning and answer text on onContent, then returns the
// assembled ChatResponse. Kobold may buffer tool turns server-side, then
// deliver the completed calls through the same SSE delta format.
func (c *Server) Stream(ctx context.Context, req llm.ChatRequest, onReasoning, onContent func(string)) (llm.ChatResponse, error) {
	httpReq, err := c.streamRequest(ctx, req)
	if err != nil {
		return llm.ChatResponse{}, err
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("call backend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return llm.ChatResponse{}, &llm.APIError{Status: resp.StatusCode, StatusText: resp.Status, Body: string(raw)}
	}
	state := streamAccumulator{calls: map[int]*deltaCall{}, progress: llm.Progress{Phase: "waiting", PromptTokens: llm.EstimatePromptTokens(req), Estimated: true}, generationKnown: true}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[len("data:"):])
		if payload == "[DONE]" {
			state.complete = true
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return llm.ChatResponse{}, fmt.Errorf("invalid stream frame: %w", err)
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return llm.ChatResponse{}, fmt.Errorf("stream backend error: %s", chunk.Error)
		}
		state.consume(chunk, req.OnProgress, onReasoning, onContent)
	}
	msg := state.message()
	if err := sc.Err(); err != nil {
		return llm.ChatResponse{Message: msg, FinishReason: "incomplete_stream"}, fmt.Errorf("read stream: %w", err)
	}
	if !state.complete {
		return llm.ChatResponse{Message: msg, FinishReason: "incomplete_stream"}, fmt.Errorf("%w: EOF before completion", llm.ErrIncompleteStream)
	}
	usage := state.usage(req)
	c.recordUsage(usage.PromptTokens, usage.CompletionTokens, usage.CachedTokens)
	p, g, n := c.UsageTotals()
	c.logDebug("[%s] --- usage (stream: %d prompt + %d generated; run totals: %d prompt + %d generated over %d calls) ---\n\n", time.Now().Format(time.RFC3339), usage.PromptTokens, usage.CompletionTokens, p, g, n)
	return llm.ChatResponse{Message: msg, FinishReason: state.finish, Usage: usage}, nil
}

func (c *Server) streamRequest(ctx context.Context, req llm.ChatRequest) (*http.Request, error) {
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
	structured := req.Grammar != "" || req.JSONSchema != ""
	if structured {
		c.dialect.applyStructured(&wreq, req.Grammar, req.JSONSchema)
	} else {
		grammar := c.Grammar
		if grammar == "" && !c.NoGrammar {
			grammar = c.dialect.contentGrammar()
		}
		if grammar != "" {
			c.dialect.applyStructured(&wreq, grammar, "")
		}
	}
	c.dialect.applySampling(&wreq, c.DRY)
	// Preserve the same image capability policy as unary requests.
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

	wreq.Stream, wreq.StreamOptions = true, &streamOptions{IncludeUsage: true}
	if _, ok := c.dialect.(llamaDialect); ok {
		wreq.ReturnProgress, wreq.TimingsPerToken = true, true
	}
	body, err := json.Marshal(wreq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
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
