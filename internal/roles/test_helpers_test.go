package roles

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/workspace"
)

type stubClient struct{}

func (stubClient) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}

func testEnv(t *testing.T) Env {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	return Env{Client: stubClient{}, WS: ws, Procs: tools.NewBackgroundProcesses()}
}

func hasTool(a interface{ ToolNames() []string }, name string) bool {
	return slices.Contains(a.ToolNames(), name)
}

// Taking the mutating tools away beats asking a weak model not to use
// them, which is the whole reason inspectors get a different registry.
// seqStub replays scripted responses in order.
type seqStub struct {
	calls int
	steps []llm.ChatResponse
}

func (s *seqStub) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	r := s.steps[s.calls]
	s.calls++
	return r, nil
}

func missingRead(id string) llm.ChatResponse {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
		{ID: id, Name: "read_file", Arguments: json.RawMessage(`{"path":"missing.go"}`)}}}}
}

// textStub answers every turn with fixed text and no tool calls.
type textStub struct{ text string }

func (s textStub) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: s.text}}, nil
}

type identityClient struct {
	t     *testing.T
	calls int
}

func (c *identityClient) Chat(_ context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	c.calls++
	if len(r.Messages) == 0 || !strings.Contains(r.Messages[0].Content, "Assistant name: Ada") || strings.Contains(r.Messages[0].Content, "You are GEKTOR") {
		c.t.Fatal(r.Messages)
	}
	return llm.ChatResponse{Message: llm.Message{Content: "Hello"}}, nil
}

// recordingClient captures every request and answers immediately.
type recordingClient struct{ reqs []llm.ChatRequest }

func (c *recordingClient) Chat(_ context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	c.reqs = append(c.reqs, r)
	return llm.ChatResponse{
		Message:      llm.Message{Role: llm.RoleAssistant, Content: "done"},
		FinishReason: "stop",
	}, nil
}

type subagentBudgetClient struct{ history []llm.Message }

func (c *subagentBudgetClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.history = req.Messages
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

type roleClientFunc func(context.Context, llm.ChatRequest) (llm.ChatResponse, error)

func (f roleClientFunc) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	return f(ctx, req)
}
