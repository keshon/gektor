package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/llm"
)

// stubSubClient returns a fixed answer with no tool calls, so spawned
// subagents finish immediately without needing real tools.
type stubSubClient struct{ answer string }

func (c stubSubClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: c.answer}}, nil
}

func TestDelegate_PassesRoleToSpawn(t *testing.T) {
	var gotRole string
	d := &Delegate{
		Spawn: func(role string) *agent.Agent {
			gotRole = role
			return agent.New(agent.Config{
				Client: stubSubClient{answer: "done"},
				Tools:  agent.NewRegistry(),
				System: "base",
			})
		},
	}

	args, _ := json.Marshal(map[string]string{
		"task": "write a greeting",
		"role": "Alice, a software engineer from London who loves tea",
	})
	out, err := d.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("result = %q, want subagent answer embedded", out)
	}
	if !strings.Contains(out, "DELEGATE\nmutations: 0") {
		t.Fatalf("expected DELEGATE header, got: %q", out)
	}
	if gotRole != "Alice, a software engineer from London who loves tea" {
		t.Fatalf("role passed to Spawn = %q, want the role text", gotRole)
	}
}

func TestDelegate_EmptyRoleIsFine(t *testing.T) {
	var gotRole string
	called := false
	d := &Delegate{
		Spawn: func(role string) *agent.Agent {
			called = true
			gotRole = role
			return agent.New(agent.Config{
				Client: stubSubClient{answer: "done"},
				Tools:  agent.NewRegistry(),
				System: "base",
			})
		},
	}

	args, _ := json.Marshal(map[string]string{"task": "write a greeting"})
	if _, err := d.Run(context.Background(), args); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called {
		t.Fatal("Spawn was never called")
	}
	if gotRole != "" {
		t.Fatalf("role = %q, want empty when not provided", gotRole)
	}
}

// Delegates write to the same workspace as the parent, so the loop must
// run them one at a time. That is what makes a semaphore inside Delegate
// unnecessary, and it is why a step making several delegate calls spawns
// sequential subagents rather than parallel ones.
func TestDelegate_IsExclusiveSoTheLoopSerialisesIt(t *testing.T) {
	var d Delegate
	if got := d.Mode(); got != agent.Exclusive {
		t.Fatalf("Mode = %v, want Exclusive", got)
	}
}

func TestDelegate_EmptyResultErrors(t *testing.T) {
	d := &Delegate{
		Spawn: func(role string) *agent.Agent {
			return agent.New(agent.Config{
				Client: stubSubClient{answer: "   "},
				Tools:  agent.NewRegistry(),
				System: "base",
			})
		},
	}
	args, _ := json.Marshal(map[string]string{"task": "work"})
	if _, err := d.Run(context.Background(), args); err == nil {
		t.Fatal("expected error for empty subagent result")
	}
}

func TestDelegate_ReportsSubagentMutations(t *testing.T) {
	writeArgs, _ := json.Marshal(map[string]string{"path": "x.txt", "content": "hi"})
	d := &Delegate{
		Spawn: func(role string) *agent.Agent {
			return agent.New(agent.Config{
				Client: &stubClientSequence{
					responses: []llm.ChatResponse{
						{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
							{ID: "w", Name: "write_file", Arguments: writeArgs},
						}}},
						{Message: llm.Message{Role: llm.RoleAssistant, Content: "wrote file"}},
					},
				},
				Tools:  agent.NewRegistry(echoWriteStub{}),
				System: "base",
			})
		},
	}
	args, _ := json.Marshal(map[string]string{"task": "write x.txt"})
	out, err := d.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "mutations: 1") {
		t.Fatalf("expected mutations count in result, got: %q", out)
	}
}

type echoWriteStub struct{}

func (echoWriteStub) Name() string            { return "write_file" }
func (echoWriteStub) Description() string     { return "write" }
func (echoWriteStub) Mode() agent.ToolMode    { return agent.Exclusive }
func (echoWriteStub) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (echoWriteStub) Run(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}

type stubClientSequence struct {
	responses []llm.ChatResponse
	calls     int
}

func (c *stubClientSequence) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	resp := c.responses[c.calls]
	c.calls++
	return resp, nil
}
