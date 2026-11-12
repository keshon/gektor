package chatcompletions

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

// Stream sends req with stream:true and reports each chunk as it arrives,
// reasoning on onReasoning and answer text on onContent, then returns the
// assembled ChatResponse. Kobold may buffer tool turns server-side, then
// deliver the completed calls through the same SSE delta format.
func (c *Client) Stream(ctx context.Context, req llm.ChatRequest, onReasoning, onContent func(string)) (llm.ChatResponse, error) {
	httpReq, err := c.request(ctx, req, true)
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

	if c.Debug != nil {
		c.logDebug("[%s] --- response stream (status %s) ---\n", time.Now().Format(time.RFC3339), resp.Status)
	}

	state := streamAccumulator{calls: map[int]*deltaCall{}, progress: llm.Progress{Phase: "waiting", PromptTokens: llm.EstimatePromptTokens(req), Estimated: true}, generationKnown: true}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		if c.Debug != nil {
			c.logDebug("%s\n", sc.Text())
		}
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
