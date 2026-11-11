package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

// mutStub stands in for any of the project's mutating file tools — the
// report only cares about the name and the path-shaped arguments.
type mutStub struct{ name string }

func (m mutStub) Name() string          { return m.name }
func (m mutStub) Description() string   { return "stub" }
func (mutStub) Mode() ToolMode          { return Exclusive }
func (mutStub) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (mutStub) Run(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}

func TestAgent_Report_RecordsMutatedPathsStepsAndFinal(t *testing.T) {
	writeArgs, _ := json.Marshal(map[string]string{"path": "a.txt", "content": "x"})
	moveArgs, _ := json.Marshal(map[string]string{"from": "b.txt", "to": "c.txt"})
	writeAgain, _ := json.Marshal(map[string]string{"path": "a.txt", "content": "y"})

	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "write_file", Arguments: writeArgs},
			{ID: "2", Name: "move_file", Arguments: moveArgs},
		}}, Usage: llm.Usage{PromptTokens: 100}},
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "3", Name: "write_file", Arguments: writeAgain},
		}}, Usage: llm.Usage{PromptTokens: 200}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"},
			Usage: llm.Usage{PromptTokens: 300}},
	}}
	a := New(Config{
		Client: client,
		Tools:  NewRegistry(mutStub{"write_file"}, mutStub{"move_file"}),
		System: "sys",
	})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	r := a.Report()
	if r.Steps != 3 {
		t.Fatalf("Steps = %d, want 3", r.Steps)
	}
	if r.Final != out || r.Final != "done" {
		t.Fatalf("Final = %q, want %q", r.Final, "done")
	}
	if r.LastPromptTokens != 300 {
		t.Fatalf("LastPromptTokens = %d, want 300", r.LastPromptTokens)
	}
	// First-touch order, a.txt not repeated for the second write.
	want := []string{"a.txt", "b.txt", "c.txt"}
	if len(r.MutatedPaths) != len(want) {
		t.Fatalf("MutatedPaths = %v, want %v", r.MutatedPaths, want)
	}
	for i, p := range want {
		if r.MutatedPaths[i] != p {
			t.Fatalf("MutatedPaths = %v, want %v", r.MutatedPaths, want)
		}
	}
}

func TestAgent_Report_PopulatedEvenWhenMaxStepsExceeded(t *testing.T) {
	writeArgs, _ := json.Marshal(map[string]string{"path": "half-done.txt"})
	client := &repeatingToolClient{args: writeArgs}
	a := New(Config{
		Client: client,
		Tools:  NewRegistry(mutStub{"write_file"}),
		System: "sys",

		MaxSteps: 2,
	})

	if _, err := a.Run(context.Background(), "task"); err == nil {
		t.Fatal("expected max-steps error")
	}

	r := a.Report()
	if r.Steps != 2 {
		t.Fatalf("Steps = %d, want 2", r.Steps)
	}
	if len(r.MutatedPaths) != 1 || r.MutatedPaths[0] != "half-done.txt" {
		t.Fatalf("MutatedPaths = %v, want [half-done.txt] — a run that hit "+
			"MaxSteps still wrote files, and the harness must know", r.MutatedPaths)
	}
	if a.LastRunMutations != 2 {
		t.Fatalf("LastRunMutations = %d, want 2", a.LastRunMutations)
	}
	if r.Final != "" {
		t.Fatalf("Final = %q, want empty for an unfinished run", r.Final)
	}
}

func TestAgent_Report_ResetBetweenRuns(t *testing.T) {
	writeArgs, _ := json.Marshal(map[string]string{"path": "first.txt"})
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "write_file", Arguments: writeArgs},
		}}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "first done"}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "second done"}},
	}}
	a := New(Config{
		Client: client,
		Tools:  NewRegistry(mutStub{"write_file"}),
		System: "sys",
	})

	if _, err := a.Run(context.Background(), "task one"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if _, err := a.Run(context.Background(), "task two"); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	r := a.Report()
	if len(r.MutatedPaths) != 0 {
		t.Fatalf("MutatedPaths = %v, want empty — report must not leak across runs", r.MutatedPaths)
	}
	if r.Final != "second done" {
		t.Fatalf("Final = %q, want %q", r.Final, "second done")
	}
}

func TestMutatedPathsFromCall(t *testing.T) {
	got := mutatedPathsFromCall(json.RawMessage(`{"from":"a","to":"b"}`))
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("move args = %v, want [a b]", got)
	}
	if got := mutatedPathsFromCall(json.RawMessage(`{"other":"x"}`)); got != nil {
		t.Fatalf("pathless args = %v, want nil", got)
	}
	if got := mutatedPathsFromCall(json.RawMessage(`not-json`)); got != nil {
		t.Fatalf("invalid args = %v, want nil", got)
	}
}

// repeatingToolClient issues the same single write_file call forever.
type repeatingToolClient struct {
	args  json.RawMessage
	calls int
}

func (c *repeatingToolClient) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	c.calls++
	return llm.ChatResponse{Message: llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{
			{ID: "r", Name: "write_file", Arguments: c.args},
		},
	}}, nil
}

func TestAgent_DelegateMutations_CountTowardVerifyZeroWrites(t *testing.T) {
	delegateArgs, _ := json.Marshal(map[string]string{"task": "write file"})
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "d", Name: "delegate_task", Arguments: delegateArgs},
		}}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "delegated"}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "confirmed"}},
	}}
	delegateTool := delegateStub{mutations: 1, result: "sub done"}
	a := New(Config{
		Client: client,
		Tools:  NewRegistry(delegateTool),
		System: "sys",
	})

	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, m := range client.lastHistory {
		if strings.Contains(m.Content, "Concrete fact: 0 file-writing") {
			t.Fatalf("delegate mutations should prevent zero-writes fact: %q", m.Content)
		}
	}
}

func TestFailedDelegateRetainsEffectsAndFailure(t *testing.T) {
	a := New(Config{MaxSteps: 2, Client: &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "d", Name: "delegate_task", Arguments: json.RawMessage(`{"task":"edit"}`)}}}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "Incomplete."}},
	}}, Tools: NewRegistry(delegateStub{mutations: 1, result: "partial evidence", err: ErrMaxSteps})})
	var result ToolResult
	a.cfg.OnResult = func(r ToolResult) { result = r }
	if _, err := a.Run(t.Context(), "test"); err != nil {
		t.Fatal(err)
	}
	if a.LastRunMutations != 1 || len(a.MutatedPaths()) != 1 || a.MutatedPaths()[0] != "changed.txt" || !result.Failed || !strings.Contains(result.Output, "partial evidence") {
		t.Fatalf("report=%+v result=%+v", a.Report(), result)
	}
}

func TestCancellationOnLastStepKeepsEffectsAndCorrectReason(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := &stubClient{responses: []llm.ChatResponse{{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "last", Name: "echo", Arguments: json.RawMessage(`{"path":"changed.txt"}`)}}}}}}
	a := New(Config{Client: client, MaxSteps: 1, Tools: NewRegistry(echoToolStub{}), MutatingTools: []string{"echo"}, OnResult: func(ToolResult) { cancel() }})
	_, err := a.Run(ctx, "test")
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrMaxSteps) || client.calls != 1 || a.LastRunMutations != 1 || a.Report().Outcome != "cancelled" {
		t.Fatalf("err=%v report=%+v", err, a.Report())
	}
}

func TestParseDelegateMutations(t *testing.T) {
	got, paths := parseDelegateMutations("DELEGATE\nmutations: 2\npaths: a.txt\npaths: b.txt\n----\nsub result")
	if got != 2 {
		t.Fatalf("parseDelegateMutations = %d, want 2", got)
	}
	if len(paths) != 2 || paths[0] != "a.txt" || paths[1] != "b.txt" {
		t.Fatalf("paths = %v", paths)
	}
	if got, paths := parseDelegateMutations("plain text"); got != 0 || paths != nil {
		t.Fatalf("unstructured content = %d %v, want 0 nil", got, paths)
	}
}
