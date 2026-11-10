package model

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

type fakeClient struct {
	req      llm.ChatRequest
	response llm.ChatResponse
}

func (c *fakeClient) Chat(_ context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	c.req = r
	return c.response, nil
}
func TestBoundedGenerationValidatesOutput(t *testing.T) {
	c := &fakeClient{response: llm.ChatResponse{Message: llm.Message{Content: `{"value":42}`}, FinishReason: "stop"}}
	n := Definition(func(context.Context, string) (Connection, error) {
		return Connection{Client: c, Name: "test", MaxTokens: 64, ContextLimit: 1024}, nil
	})
	args := json.RawMessage(`{"profile":"local.json","prompt":"Return a value","input":{"x":1},"max_tokens":128,"output_schema":{"type":"object","required":["value"]}}`)
	if _, err := n.Execute(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	if c.req.MaxTokens != 64 || len(c.req.Tools) != 0 || len(c.req.Messages) != 1 {
		t.Fatal(c.req)
	}
	c.response.Message.Content = `{}`
	if _, err := n.Execute(t.Context(), args); err == nil {
		t.Fatal("invalid structured output accepted")
	}
	c.response.FinishReason = "length"
	if _, err := n.Execute(t.Context(), args); err == nil {
		t.Fatal("truncated output accepted")
	}
}
