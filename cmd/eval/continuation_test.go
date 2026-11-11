package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/tools"
)

type continuationClient struct{ t *testing.T }

func (c continuationClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.t.Helper()
	users := 0
	found := false
	for _, msg := range req.Messages {
		if msg.Role == llm.RoleUser && msg.Harness == "" {
			users++
			if msg.Content != "opening request" {
				c.t.Fatal("invented user message", msg)
			}
		}
		if msg.Content == "user-like assistant text" {
			found = msg.Role == llm.RoleAssistant
		}
	}
	if users != 1 || !found || req.Messages[0].Role != llm.RoleSystem || req.Messages[0].Content == "" || req.Messages[len(req.Messages)-1].Role != llm.RoleTool {
		c.t.Fatal("roles or system prompt changed", req.Messages)
	}
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

func TestContinuationPreservesRolesWithoutNewUserTurn(t *testing.T) {
	ws := checkWorkspaceForTest(t)
	procs := tools.NewBackgroundProcesses()
	defer procs.StopAll()
	p := Probe{Task: "opening request", Continuation: []llm.Message{
		{Role: llm.RoleAssistant, Content: "user-like assistant text", ToolCalls: []llm.ToolCall{{ID: "recorded", Name: "system_info", Arguments: json.RawMessage(`{"group":"tools"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "recorded", Content: `{"tools":[]}`},
	}}
	var result Result
	env := evalEnv(continuationClient{t}, ws, procs, 131072, 2048, func(int, llm.Message) {}, func(string) {})
	if got := executeProbeAgent(t.Context(), p, env, nil, &result); got != "done" || result.RunError != "" {
		t.Fatal(got, result.RunError)
	}
}

func TestProbeRetainsConstructionFailure(t *testing.T) {
	ws := checkWorkspaceForTest(t)
	procs := tools.NewBackgroundProcesses()
	defer procs.StopAll()
	env := evalEnv(continuationClient{t}, ws, procs, 131072, 2048, func(int, llm.Message) {}, func(string) {})
	env.NativeExposure = map[string]tools.Exposure{"unknown-package": tools.Eager}
	var result Result
	answer := executeProbeAgent(t.Context(), Probe{Task: "inspect"}, env, nil, &result)
	if answer != "" || !strings.Contains(result.RunError, "unsupported native exposure") {
		t.Fatal(answer, result.RunError)
	}
}
