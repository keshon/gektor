package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

type pictureTool struct {
	paths []string
}

func (pictureTool) Name() string            { return "read_picture" }
func (pictureTool) Description() string     { return "picture" }
func (pictureTool) Mode() ToolMode          { return Concurrent }
func (pictureTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (pictureTool) Run(context.Context, json.RawMessage) (string, error) {
	return "FILE\nimage: true\nattached: true", nil
}
func (p pictureTool) Attach(json.RawMessage) ([]string, error) { return p.paths, nil }

func toolCallStep(name string) llm.ChatResponse {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
		{ID: "pic", Name: name, Arguments: json.RawMessage(`{"path":"shot.png"}`)},
	}}}
}

func TestToolPicturesRideTheTurnAfterToolResults(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		toolCallStep("read_picture"),
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "seen"}},
	}}
	a := New(Config{
		Client: client,
		Tools:  NewRegistry(pictureTool{paths: []string{`C:\ws\shot.png`}}),
		System: "sys",
	})
	if _, err := a.Run(context.Background(), "look"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var toolAt = -1
	for i, msg := range client.lastHistory {
		if msg.Role == llm.RoleTool {
			if len(msg.Images) != 0 {
				t.Fatalf("tool message carries images: %v", msg.Images)
			}
			toolAt = i
		}
	}
	if toolAt < 0 || toolAt+1 >= len(client.lastHistory) {
		t.Fatalf("no message after the tool result: %v", client.lastHistory)
	}
	pic := client.lastHistory[toolAt+1]
	if pic.Role != llm.RoleUser || len(pic.Images) != 1 || pic.Images[0] != `C:\ws\shot.png` {
		t.Fatalf("picture turn = %+v", pic)
	}
	if !strings.Contains(pic.Content, "tool output") || !strings.Contains(pic.Content, "not a new user request") {
		t.Fatalf("picture turn does not label its origin: %q", pic.Content)
	}
}

func TestToolPicturesAreCappedPerStep(t *testing.T) {
	paths := []string{"a.png", "b.png", "c.png", "d.png", "e.png"}
	client := &stubClient{responses: []llm.ChatResponse{
		toolCallStep("read_picture"),
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(pictureTool{paths: paths}), System: "sys"})
	if _, err := a.Run(context.Background(), "look"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var tool *llm.Message
	for i := range client.lastHistory {
		if client.lastHistory[i].Role == llm.RoleTool {
			tool = &client.lastHistory[i]
		}
		if len(client.lastHistory[i].Images) > 0 {
			t.Fatalf("over-cap pictures were attached: %v", client.lastHistory[i].Images)
		}
	}
	if tool == nil || !strings.Contains(tool.Content, "at most 4 pictures") {
		t.Fatalf("cap refusal missing: %+v", tool)
	}
	if tool.ToolFailed == nil || !*tool.ToolFailed {
		t.Fatalf("over-cap call not marked failed")
	}
}
