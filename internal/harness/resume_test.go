package harness

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
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
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "question", Name: "ask_user", Arguments: json.RawMessage(`{"question":"which option?"}`)}}},
			}
			sess, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			answer, err := sess.Resume(t.Context(), history, "continue")
			if err != nil || answer != "done" || sess.Report().Final != "done" {
				t.Fatal(answer, sess.Report(), err)
			}
			if supplied && asked != 0 || !supplied && asked != 1 {
				t.Fatalf("asked=%d supplied=%v", asked, supplied)
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
				t.Fatal("saved question answer did not reach the resumed request")
			}
		})
	}
}
