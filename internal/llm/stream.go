package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrIncompleteStream reports a stream the backend ended early. A caller
// that has not shown any of the text yet can retry the turn unary; the
// same failure mid-request is retryable transport on that path.
var ErrIncompleteStream = errors.New("incomplete stream")

// ErrStreamUnsupported reports a backend with no streaming endpoint, so a
// caller can fall back to a unary call instead of failing the turn.
var ErrStreamUnsupported = errors.New("streaming unsupported")

// Stream sends req with stream:true and reports each chunk as it arrives,
// reasoning on onReasoning and answer text on onContent, then returns the
// assembled ChatResponse. Only dialects with SSE support work: koboldcpp
// decides tool calls in its own server-side pass and has no stable stream
// contract, so it refuses and the caller falls back to ChatWithRetry.
func (c *Server) Stream(ctx context.Context, req ChatRequest, onReasoning, onContent func(string)) (ChatResponse, error) {
	if _, ok := c.dialect.(koboldDialect); ok {
		return ChatResponse{}, fmt.Errorf("%w on koboldcpp — use Chat", ErrStreamUnsupported)
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
	structured := req.Grammar != "" || req.JSONSchema != ""
	if structured {
		c.dialect.applyStructured(&wreq, req.Grammar, req.JSONSchema)
	}
	c.dialect.applySampling(&wreq, c.DRY)
	// Koboldcpp never reaches here (refused above), so images are
	// always encodable on the streaming path.
	msgs, err := encodeMessages(req.Messages, true)
	if err != nil {
		return ChatResponse{}, err
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
	body, err := json.Marshal(wreq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatURL(), bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("build request: %w", err)
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

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("call backend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return ChatResponse{}, &APIError{Status: resp.StatusCode, StatusText: resp.Status, Body: string(raw)}
	}

	var content strings.Builder
	var reasoning strings.Builder
	var finish string
	complete := false
	type deltaCall struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	acc := map[int]*deltaCall{}
	var promptTokens, completionTokens int
	var cachedTokens int

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[len("data:"):])
		if payload == "[DONE]" {
			complete = true
			break
		}
		var chunk struct {
			Error   json.RawMessage `json:"error"`
			Choices []struct {
				Delta struct {
					Role      string      `json:"role"`
					Content   string      `json:"content"`
					Reasoning string      `json:"reasoning_content"`
					ToolCalls []deltaCall `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *wireUsage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return ChatResponse{}, fmt.Errorf("invalid stream frame: %w", err)
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return ChatResponse{}, fmt.Errorf("stream backend error: %s", chunk.Error)
		}
		if chunk.Usage != nil {
			promptTokens = chunk.Usage.PromptTokens
			completionTokens = chunk.Usage.CompletionTokens
			cachedTokens = chunk.Usage.PromptTokensDetails.CachedTokens
		}
		for _, ch := range chunk.Choices {
			if ch.FinishReason != "" {
				finish = ch.FinishReason
				complete = true
			}
			if ch.Delta.Content != "" {
				content.WriteString(ch.Delta.Content)
				if onContent != nil {
					onContent(ch.Delta.Content)
				}
			}
			// Reasoning is reported as it arrives, separately from content, so a
			// reader watches deliberation finish before the answer begins.
			// It used to accumulate silently and appear only once the whole
			// reply was done.
			if ch.Delta.Reasoning != "" {
				reasoning.WriteString(ch.Delta.Reasoning)
				if onReasoning != nil {
					onReasoning(ch.Delta.Reasoning)
				}
			}
			for _, dc := range ch.Delta.ToolCalls {
				a := acc[dc.Index]
				if a == nil {
					cp := dc
					a = &cp
					acc[dc.Index] = a
				} else {
					a.Function.Arguments += dc.Function.Arguments
					if dc.ID != "" {
						a.ID = dc.ID
					}
					if dc.Function.Name != "" {
						a.Function.Name = dc.Function.Name
					}
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return ChatResponse{}, fmt.Errorf("read stream: %w", err)
	}
	if !complete {
		return ChatResponse{}, fmt.Errorf("%w: EOF before completion", ErrIncompleteStream)
	}

	msg := Message{Role: RoleAssistant, Content: content.String(), Reasoning: reasoning.String()}
	for i := 0; i < len(acc); i++ {
		dc, ok := acc[i]
		if !ok {
			continue
		}
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID:        dc.ID,
			Name:      dc.Function.Name,
			Arguments: normalizeArguments(json.RawMessage(dc.Function.Arguments)),
		})
	}
	// A backend may omit the requested usage block. Fall back to an estimate
	// from the wire, marked Estimated (displays show "~").
	//
	// Count what goes on the wire and nothing else. Reasoning is decoded
	// for display and never encoded back, and tool-call arguments are
	// routinely larger than the text around them, so counting one and
	// skipping the other inflated the prompt on every step of a reasoning
	// run. Every budget downstream trusts this number: compaction fired
	// early and the response allowance shrank for no reason.
	estimated := false
	if promptTokens == 0 {
		estimated = true
		n := 0
		for _, m := range req.Messages {
			n += len([]rune(m.Content))
			n += len(m.Images) * imageEstimateTokens
			for _, call := range m.ToolCalls {
				n += len([]rune(call.Name)) + len(call.Arguments)
			}
		}
		if toolsJSON, err := json.Marshal(req.Tools); err == nil {
			n += len([]rune(string(toolsJSON)))
		}
		promptTokens = n / 4
	}
	if completionTokens == 0 {
		estimated = true
		completionTokens = (len([]rune(content.String())) + len([]rune(reasoning.String()))) / 4
	}
	c.recordUsage(promptTokens, completionTokens, cachedTokens)
	p, g, n := c.UsageTotals()
	c.logDebug("[%s] --- usage (stream: %d prompt + %d generated; run totals: %d prompt + %d generated over %d calls) ---\n\n",
		time.Now().Format(time.RFC3339), promptTokens, completionTokens, p, g, n)
	return ChatResponse{Message: msg, FinishReason: finish, Usage: Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		CachedTokens:     cachedTokens,
		Estimated:        estimated,
	}}, nil
}
