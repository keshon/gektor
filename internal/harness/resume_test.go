package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

type resumeClient struct{ requests []llm.ChatRequest }

func (c *resumeClient) Chat(_ context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	c.requests = append(c.requests, request)
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}, FinishReason: "stop"}, nil
}

func TestResumeSharesSavedAnswerPromptAndReport(t *testing.T) {
	for _, supplied := range []bool{false, true} {
		t.Run(map[bool]string{false: "prompt", true: "supplied-answer"}[supplied], func(t *testing.T) {
			client := &resumeClient{}
			cfg := Config{Env: testEnv(t, client, nil), StateFile: filepath.Join(t.TempDir(), "state.json"), AskPrompt: func(question string) string { return "saved question: " + question }}
			asked := 0
			cfg.Answer = func(_ context.Context, request agent.SuspendRequest) (agent.SuspendReply, error) {
				asked++
				if !strings.HasPrefix(request.Prompt, "saved question:") {
					t.Fatal(request.Prompt)
				}
				return agent.SuspendReply{Answer: "selected answer"}, nil
			}
			if supplied {
				cfg.ResumeAnswer = "selected answer"
			}
			history := []llm.Message{
				{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: "task"},
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
					{ID: "question", Name: "ask_user", Arguments: json.RawMessage(`{"question":"which option?"}`)},
					{ID: "sibling", Name: "write_file", Arguments: json.RawMessage(`{"path":"unsafe.txt","content":"must not execute"}`)},
				}},
			}
			sess, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			const note = "Do not deploy. Only inspect staging."
			image := filepath.Join(t.TempDir(), "new-reference.png")
			answer, err := sess.Resume(t.Context(), history, note, image)
			if err != nil || answer != "done" || sess.Report().Final != "done" {
				t.Fatal(answer, sess.Report(), err)
			}
			if supplied && asked != 0 || !supplied && asked != 1 {
				t.Fatalf("asked=%d supplied=%v", asked, supplied)
			}
			saved, err := conversation.Load(cfg.StateFile)
			if err != nil {
				t.Fatal(err)
			}
			if len(client.requests) != 1 {
				t.Fatalf("model calls = %d", len(client.requests))
			}
			for _, messages := range [][]llm.Message{client.requests[0].Messages, saved.History} {
				questionAt, siblingAt, noteAt := -1, -1, -1
				for index, message := range messages {
					switch {
					case message.Role == llm.RoleTool && message.ToolCallID == "question":
						if message.Content != "selected answer" || message.ToolFailed == nil || *message.ToolFailed {
							t.Fatalf("question reply = %+v", message)
						}
						questionAt = index
					case message.Role == llm.RoleTool && message.ToolCallID == "sibling":
						if message.ToolFailed == nil || !*message.ToolFailed {
							t.Fatalf("interrupted sibling was not closed as unknown: %+v", message)
						}
						siblingAt = index
					case message.Role == llm.RoleUser && message.Content == note:
						if len(message.Images) != 1 || message.Images[0] != image {
							t.Fatal("resume lost attached image", message.Images)
						}
						noteAt = index
					}
				}
				if questionAt < 0 || siblingAt < 0 || noteAt <= questionAt || noteAt <= siblingAt {
					t.Fatalf("tool group must close before new input: question=%d sibling=%d note=%d", questionAt, siblingAt, noteAt)
				}
			}
			_, err = cfg.Env.WS.Stat("unsafe.txt")
			if !os.IsNotExist(err) {
				t.Fatal("interrupted sibling executed", err)
			}
		})
	}
}
