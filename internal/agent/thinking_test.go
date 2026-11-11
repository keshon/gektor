package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestAgent_ThinkUnderBudgetFinishes(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "<think>brief</think>done"}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys"})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "<think>brief</think>done" {
		t.Fatalf("result = %q", out)
	}
	for _, m := range client.lastHistory {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "Reasoning budget exceeded") {
			t.Fatal("no wrap-up should fire under budget")
		}
	}
}

func TestAgent_ThinkBudgetDisabled(t *testing.T) {
	ramble := "<think>" + strings.Repeat("x. ", 4000) + "</think>done anyway"
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: ramble}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys"})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != ramble {
		t.Fatalf("disabled budget must finish as-is")
	}
}

func TestAgent_ThinkWithToolCallsNotWrapped(t *testing.T) {
	// Acting is progress: a thinking model that also calls tools is
	// converging, not rambling. Only finishes wrap.
	ramble := "<think>" + strings.Repeat("thinking hard. ", 400) + "</think>acting"
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{
			Role:    llm.RoleAssistant,
			Content: ramble,
			ToolCalls: []llm.ToolCall{
				{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{}`)},
			},
		}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(echoToolStub{}), System: "sys"})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "done" {
		t.Fatalf("result = %q, want done", out)
	}
	for _, m := range client.lastHistory {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "Reasoning budget exceeded") {
			t.Fatal("acting steps must not wrap")
		}
	}
}
