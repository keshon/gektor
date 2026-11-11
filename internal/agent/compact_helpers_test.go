package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

type largeRead struct{}

func (largeRead) Name() string { return "large_read" }

func (largeRead) Description() string { return "read" }

func (largeRead) Schema() json.RawMessage { return json.RawMessage(`{}`) }

func (largeRead) Mode() ToolMode { return Concurrent }

func (largeRead) Run(context.Context, json.RawMessage) (string, error) {
	return strings.Repeat("new ", 5000), nil
}

func assistantStep(id string) []llm.Message {
	return []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: "echo"}}},
		{Role: llm.RoleTool, ToolCallID: id, Content: "ok"},
	}
}

// history builds a transcript with n assistant-led step groups after the
// system/user prefix, which is the shape compactHistory splits on.
func historyWithSteps(n int) []llm.Message {
	out := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "task"},
	}
	for i := 0; i < n; i++ {
		out = append(out,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c", Name: "read_file"}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: "c", Content: "contents"},
		)
	}
	return out
}

func compactAgent(t *testing.T, atPercent int) *Agent {
	t.Helper()
	return New(Config{
		Client: &stubClient{}, Tools: NewRegistry(), System: "sys",
		ContextLimit: 1000, CompactKeepSteps: 2, CompactAtPercent: atPercent,
	})
}
