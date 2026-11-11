package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

// stubSubClient returns a fixed answer with no tool calls, so spawned
// subagents finish immediately without needing real tools.
type stubSubClient struct{ answer string }

func (c stubSubClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: c.answer}}, nil
}

type delegateClientFunc func(context.Context, llm.ChatRequest) (llm.ChatResponse, error)

func (f delegateClientFunc) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	return f(ctx, req)
}

func TestDelegatePartialEvidenceOnEveryExit(t *testing.T) {
	for _, stop := range []string{"step_limit", "cancelled", "deadline_exceeded", "failed"} {
		t.Run(stop, func(t *testing.T) {
			calls := 0
			archive := filepath.Join(t.TempDir(), "state.json")
			d := &Delegate{DefaultSteps: 1, Spawn: func(ctx context.Context, role string, steps int) (*agent.Agent, error) {
				limit := steps
				if stop != "step_limit" {
					limit = 2
				}
				return agent.New(agent.Config{MaxSteps: limit, StateFile: archive, MutatingTools: []string{"write_file"}, Tools: agent.NewRegistry(echoWriteStub{}), Client: delegateClientFunc(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
					calls++
					if calls == 1 {
						return llm.ChatResponse{Usage: llm.Usage{PromptTokens: 9, CompletionTokens: 3}, Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "edit", Name: "write_file", Arguments: json.RawMessage(`{"path":"x.txt","content":"changed"}`)}}}}, nil
					}
					switch stop {
					case "cancelled":
						return llm.ChatResponse{}, context.Canceled
					case "deadline_exceeded":
						return llm.ChatResponse{}, context.DeadlineExceeded
					default:
						return llm.ChatResponse{}, errors.New("backend failed")
					}
				})}), nil
			}}
			out, err := d.Run(t.Context(), json.RawMessage(`{"task":"edit"}`))
			if err == nil {
				t.Fatal("failure lost")
			}
			for _, want := range []string{"termination: " + stop, "mutations: 1", "paths: x.txt", "tool_result: edit write_file success", "archive: " + archive, "prompt=9 completion=3"} {
				if !strings.Contains(out, want) {
					t.Fatalf("missing %q in %s", want, out)
				}
			}
			wantCalls := 2
			if stop == "step_limit" {
				wantCalls = 1
				if !errors.Is(err, agent.ErrMaxSteps) {
					t.Fatal(err)
				}
			}
			if calls != wantCalls {
				t.Fatalf("unexpected continuation: %d calls", calls)
			}
		})
	}
}

func TestDelegateBudgetBoundsBeforeSpawn(t *testing.T) {
	for _, tc := range []struct {
		args string
		want int
	}{
		{`{"task":"review"}`, 4}, {`{"task":"review","max_steps":7}`, 7},
		{`{"task":"review","max_steps":0}`, 0}, {`{"task":"review","max_steps":-1}`, 0},
		{`{"task":"review","max_steps":9}`, 0}, {`{"task":"review","max_steps":1.5}`, 0},
	} {
		t.Run(tc.args, func(t *testing.T) {
			got := 0
			d := &Delegate{DefaultSteps: 4, MaxSteps: 8, Spawn: func(ctx context.Context, role string, steps int) (*agent.Agent, error) {
				got = steps
				return agent.New(agent.Config{MaxSteps: steps, Client: stubSubClient{answer: "done"}, Tools: agent.NewRegistry()}), nil
			}}
			out, err := d.Run(t.Context(), json.RawMessage(tc.args))
			if got != tc.want || (err != nil) != (tc.want == 0) {
				t.Fatalf("spawned=%d err=%v", got, err)
			}
			if err == nil && !strings.Contains(out, "termination: completed") {
				t.Fatal(out)
			}
		})
	}
}

func TestDelegateReportDoesNotHideToolFailure(t *testing.T) {
	d := &Delegate{Spawn: func(ctx context.Context, role string, steps int) (*agent.Agent, error) {
		return agent.New(agent.Config{MaxSteps: 1, Tools: agent.NewRegistry(), Client: &stubClientSequence{responses: []llm.ChatResponse{{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "check", Name: "missing_check", Arguments: json.RawMessage(`{}`)}}}}}}}), nil
	}}
	out, err := d.Run(t.Context(), json.RawMessage(`{"task":"check"}`))
	if !errors.Is(err, agent.ErrMaxSteps) || !strings.Contains(out, "tool_result: check missing_check rejected") || strings.Contains(out, "termination: completed") {
		t.Fatalf("%s %v", out, err)
	}
}

func TestDelegateFailedCheckHasArchiveReference(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "state.json")
	d := &Delegate{Spawn: func(ctx context.Context, role string, steps int) (*agent.Agent, error) {
		return agent.New(agent.Config{MaxSteps: 1, StateFile: archive, Tools: agent.NewRegistry(delegateFailedCheck{}), Client: &stubClientSequence{responses: []llm.ChatResponse{{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "go-test", Name: "run_program", Arguments: json.RawMessage(`{}`)}}}}}}}), nil
	}}
	out, err := d.Run(t.Context(), json.RawMessage(`{"task":"test"}`))
	if !errors.Is(err, agent.ErrMaxSteps) || !strings.Contains(out, "tool_result: go-test run_program execution_error") || !strings.Contains(out, "archive: "+archive) {
		t.Fatalf("%s %v", out, err)
	}
	saved, loadErr := conversation.Load(archive)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	found := false
	for _, msg := range saved.History {
		if msg.ToolCallID == "go-test" && strings.Contains(msg.Content, "exit_code: 1") && msg.ToolFailed != nil && *msg.ToolFailed {
			found = true
		}
	}
	if !found {
		t.Fatal("failed check evidence missing from referenced archive")
	}
}

type delegateFailedCheck struct{ echoWriteStub }

func (delegateFailedCheck) Name() string { return "run_program" }
func (delegateFailedCheck) Run(context.Context, json.RawMessage) (string, error) {
	return "exit_code: 1\nFAIL boundary test", errors.New("check failed")
}

func TestDelegateEnvelopeIsBounded(t *testing.T) {
	r := agent.RunReport{Summary: strings.Repeat("s", 100000), OpenTodos: []string{strings.Repeat("t", 100000)}, Verification: strings.Repeat("v", 100000)}
	for i := 0; i < 1000; i++ {
		r.MutatedPaths = append(r.MutatedPaths, strings.Repeat("p", 1000))
	}
	for i := 0; i < 24; i++ {
		r.RecentTools = append(r.RecentTools, agent.ToolEvidence{CallID: strings.Repeat("i", 1000), Name: strings.Repeat("n", 1000), Status: strings.Repeat("e", 1000)})
	}
	out := formatDelegateReport(r, 1000, strings.Repeat("a", 100000), agent.ErrMaxSteps, 0)
	if len(out) > 40*1024 || !strings.Contains(out, "paths_omitted: 968") || !strings.Contains(out, "termination: step_limit") {
		t.Fatalf("unbounded or incomplete report: %d bytes", len(out))
	}
}

func TestDelegate_PassesRoleToSpawn(t *testing.T) {
	var gotRole string
	d := &Delegate{
		Spawn: func(ctx context.Context, role string, steps int) (*agent.Agent, error) {
			gotRole = role
			return agent.New(agent.Config{
				Client: stubSubClient{answer: "done"},
				Tools:  agent.NewRegistry(),
				System: "base",
			}), nil
		},
	}

	args, _ := json.Marshal(map[string]string{
		"task": "write a greeting",
		"role": "Go test coverage",
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
	if gotRole != "Go test coverage" {
		t.Fatalf("role passed to Spawn = %q, want the role text", gotRole)
	}
}

func TestDelegate_EmptyRoleIsFine(t *testing.T) {
	var gotRole string
	called := false
	d := &Delegate{
		Spawn: func(ctx context.Context, role string, steps int) (*agent.Agent, error) {
			called = true
			gotRole = role
			return agent.New(agent.Config{
				Client: stubSubClient{answer: "done"},
				Tools:  agent.NewRegistry(),
				System: "base",
			}), nil
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
		Spawn: func(ctx context.Context, role string, steps int) (*agent.Agent, error) {
			return agent.New(agent.Config{
				Client: stubSubClient{answer: "   "},
				Tools:  agent.NewRegistry(),
				System: "base",
			}), nil
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
		Spawn: func(ctx context.Context, role string, steps int) (*agent.Agent, error) {
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
			}), nil
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

func TestDelegateConstructionFailure(t *testing.T) {
	failure := errors.New("invalid child tool composition")
	d := &Delegate{Spawn: func(context.Context, string, int) (*agent.Agent, error) { return nil, failure }}
	out, err := d.Run(t.Context(), json.RawMessage(`{"task":"inspect"}`))
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "create subagent") || out != "" {
		t.Fatalf("output=%q error=%v", out, err)
	}
}
