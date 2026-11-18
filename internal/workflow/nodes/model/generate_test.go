package model

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/keshon/gektor/internal/workflow"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

type fakeClient struct {
	err      error
	req      llm.ChatRequest
	response llm.ChatResponse
}

func (c *fakeClient) Chat(_ context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	c.req = r
	return c.response, c.err
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

func TestGenerationFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response llm.ChatResponse
		err      error
		schema   string
		code     string
	}{
		{name: "provider", err: errors.New("provider unavailable"), code: "model_request_failed"},
		{name: "cancelled", err: context.Canceled},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "incomplete", response: llm.ChatResponse{FinishReason: "length"}, code: "model_incomplete"},
		{name: "tool call", response: llm.ChatResponse{Message: llm.Message{ToolCalls: []llm.ToolCall{{}}}}, code: "model_incomplete"},
		{name: "oversized", response: llm.ChatResponse{Message: llm.Message{Content: strings.Repeat("x", (1<<20)+1)}}, code: "model_response_too_large"},
		{name: "output contract", response: llm.ChatResponse{Message: llm.Message{Content: `{}`}}, schema: `{"type":"object","required":["value"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeClient{response: tc.response, err: tc.err}
			node := Definition(func(context.Context, string) (Connection, error) {
				return Connection{Client: client, Name: "fixture"}, nil
			})
			args := json.RawMessage(`{"profile":"local.json","prompt":"test"}`)
			if tc.schema != "" {
				args = json.RawMessage(`{"profile":"local.json","prompt":"test","output_schema":` + tc.schema + `}`)
			}

			_, err := node.Execute(t.Context(), args)
			if err == nil {
				t.Fatal("failure accepted")
			}
			failure := workflow.OperationFailure(err)
			if tc.code == "" {
				if failure != nil {
					t.Fatalf("non-operation failure routed: %+v", failure)
				}
			} else if failure == nil || failure.Code != tc.code {
				t.Fatalf("failure=%+v want=%s", failure, tc.code)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatal("lost original cause", err)
			}
			if node.RetrySafe {
				t.Fatal("model retries enabled")
			}
		})
	}

	node := Definition(func(context.Context, string) (Connection, error) { return Connection{}, errors.New("profile missing") })
	_, err := node.Execute(t.Context(), json.RawMessage(`{"profile":"missing","prompt":"test"}`))
	if err == nil || workflow.OperationFailure(err) != nil {
		t.Fatal("profile failure routed", err)
	}
}
