package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/workspace"
)

type launchClient struct {
	requests []llm.ChatRequest
	streams  int
	write    bool
}

func (c *launchClient) Chat(_ context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	c.requests = append(c.requests, request)
	if c.write && len(c.requests) == 1 {
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "write", Name: "write_file", Arguments: json.RawMessage(`{"path":"notes.txt","content":"key = AKIAIOSFODNN7EXAMPLE\n"}`)}}}}, nil
	}
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}, FinishReason: "stop"}, nil
}

func (c *launchClient) Stream(ctx context.Context, request llm.ChatRequest, _ func(string), content func(string)) (llm.ChatResponse, error) {
	c.streams++
	response, err := c.Chat(ctx, request)
	if content != nil {
		content(response.Message.Content)
	}
	return response, err
}

// Exercise the same constructor and session factory Run uses, without a terminal.
func TestTUIProductionFactoryPreservesLaunchBehavior(t *testing.T) {
	for _, plan := range []bool{false, true} {
		t.Run(map[bool]string{false: "act", true: "plan"}[plan], func(t *testing.T) {
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			client := &launchClient{write: true}
			verified := 0
			cfg := Config{
				Env:  roles.Env{Client: client, WS: ws, Identity: "unique-tui-identity", Model: "tui-model", ContextLimit: 65536, Temperature: 0.73, MaxTokens: 256, MaxSteps: 6, Stream: true, Vision: true, VisionKnown: true, Rules: checks.Secrets()},
				Plan: plan, Task: "write a note", StateFile: filepath.Join(t.TempDir(), "state.json"),
				Verify: func(context.Context) (string, error) { verified++; return "verified-tui", nil },
				Images: []string{filepath.Join(ws.Root(), "new.png")},
			}
			m := newModel(t.Context(), cfg)
			defer m.cancel()
			sess, err := m.newSession(cfg.Task, cfg.StateFile, cfg.Images)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := sess.Run(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(client.requests) == 0 || client.streams == 0 {
				t.Fatal("TUI lost streaming/client")
			}
			request := client.requests[0]
			if request.Temperature != 0.73 {
				t.Fatal("TUI lost temperature", request.Temperature)
			}
			var prompt strings.Builder
			attached := false
			for _, message := range request.Messages {
				prompt.WriteString(message.Content)
				if message.Role == llm.RoleUser && message.Content == cfg.Task && len(message.Images) == 1 && message.Images[0] == cfg.Images[0] {
					attached = true
				}
			}
			if !attached {
				t.Fatal("TUI lost task/images")
			}
			if !strings.Contains(prompt.String(), cfg.Env.Identity) {
				t.Fatal("TUI lost identity")
			}
			seen := map[string]int{}
			readOnly := false
			for len(m.events) > 0 {
				ev := <-m.events
				seen[ev.Name]++
				if ev.Name == "tool_result" {
					text, _ := ev.Fields["text"].(string)
					readOnly = strings.Contains(text, "read-only")
				}
			}
			if seen["delta"] == 0 || seen["tool_result"] != 1 || seen["outcome"] != 1 {
				t.Fatal("TUI lost or duplicated session events", seen)
			}
			_, statErr := os.Stat(filepath.Join(ws.Root(), "notes.txt"))
			if plan {
				if !os.IsNotExist(statErr) || !readOnly || verified != 0 {
					t.Fatal("TUI plan allowed mutation/verification", statErr, readOnly, verified)
				}
			} else if statErr != nil || seen["finding"] == 0 || verified != 1 || sess.Report().Verification != "verified-tui" {
				t.Fatal("TUI lost checks/verification", statErr, seen, verified, sess.Report())
			}
		})
	}
}

func TestTUIProductionFactoryPreservesSavedAnswer(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	client := &launchClient{}
	m := newModel(ctx, Config{Env: roles.Env{Client: client, WS: ws}, ResumeAnswer: "selected answer"})
	defer m.cancel()
	sess, err := m.newSession("", filepath.Join(t.TempDir(), "state.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: "task"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "question", Name: "ask_user", Arguments: json.RawMessage(`{"question":"which option?"}`)}}},
	}
	if _, err := sess.Resume(ctx, history, "continue"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, request := range client.requests {
		for _, message := range request.Messages {
			if message.Role == llm.RoleTool && message.ToolCallID == "question" && strings.Contains(message.Content, "selected answer") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("TUI lost saved answer")
	}
}
