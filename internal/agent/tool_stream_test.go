package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

type streamingEcho struct{ echoToolStub }

func (streamingEcho) RunStreaming(_ context.Context, _ json.RawMessage, emit func(string)) (string, error) {
	emit("preview only")
	return "authoritative result", nil
}

func TestToolPreviewPrecedesResultAndNeverEntersHistory(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call", Name: "echo", Arguments: json.RawMessage(`{}`)}}}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	var observed []string
	a := New(Config{Client: client, Tools: NewRegistry(streamingEcho{}), System: "sys",
		OnToolOutput: func(id, text string) { observed = append(observed, id+":"+text) },
		OnResult:     func(result ToolResult) { observed = append(observed, result.CallID+":"+result.Output) },
	})
	if _, err := a.Run(t.Context(), "task"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(observed, []string{"call:preview only", "call:authoritative result"}) {
		t.Fatalf("events=%v", observed)
	}
	found := false
	for _, msg := range client.lastHistory {
		if strings.Contains(msg.Content, "preview only") {
			t.Fatal("preview polluted model history")
		}
		if msg.Role == llm.RoleTool && msg.Content == "authoritative result" {
			found = true
		}
	}
	if !found {
		t.Fatal("final tool result missing from history")
	}
}
