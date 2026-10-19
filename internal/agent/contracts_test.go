package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/permission"
)

type auditTool struct{ ran *bool }

func (auditTool) Name() string            { return "audit_tool" }
func (auditTool) Description() string     { return "audit" }
func (auditTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (auditTool) Mode() ToolMode          { return Exclusive }
func (t auditTool) Run(context.Context, json.RawMessage) (string, error) {
	*t.ran = true
	return "ok", nil
}

func TestPolicyCannotBeBypassed(t *testing.T) {
	for _, hook := range []bool{false, true} {
		ran := false
		cfg := Config{Tools: NewRegistry(auditTool{&ran}), Policy: permission.Policy{Rules: []permission.Rule{{Tool: "*", Pattern: "*", Effect: permission.Deny}}}}
		if hook {
			cfg.BeforeToolCall = func(context.Context, string, string, json.RawMessage) (permission.Effect, error) {
				return permission.Allow, nil
			}
		}
		a := New(cfg)
		a.executeToolCalls(context.Background(), &runState{}, 0, "stop", []llm.ToolCall{{ID: "x", Name: "audit_tool", Arguments: json.RawMessage(`{}`)}}, nil)
		if ran {
			t.Fatal("denied tool executed")
		}

	}
}

func TestFinalAnswerDoesNotTriggerSpeculativeRetries(t *testing.T) {
	c := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "I created notes.txt"}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "I created notes.txt"}},
	}}
	c.responses = append(c.responses, llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "incomplete"}})
	a := New(Config{Client: c, Tools: NewRegistry(), MaxSteps: 2})
	out, err := a.Run(context.Background(), "Create notes.txt")
	if err != nil || out != "I created notes.txt" || c.calls != 1 || a.LastRunMutations != 0 {
		t.Fatalf("repro failed %q %v", out, err)
	}

}

func TestVerificationAfterLastMutation(t *testing.T) {
	ran := false
	checks := 0
	c := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "audit_tool", Arguments: json.RawMessage(`{}`)}}}},
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "2", Name: "audit_tool", Arguments: json.RawMessage(`{"edit":2}`)}}}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	c.responses = append(c.responses, llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "confirmed"}})
	a := New(Config{Client: c, Tools: NewRegistry(auditTool{&ran}), MutatingTools: []string{"audit_tool"}, Verify: func(context.Context) (string, error) { checks++; return "PASSED", nil }})
	_, err := a.Run(context.Background(), "edit and verify")
	if err != nil || checks != 1 || c.calls != 3 || a.LastRunMutations != 2 {
		t.Fatalf("repro failed checks=%d mutations=%d err=%v", checks, a.LastRunMutations, err)
	}

}

func say(text string) llm.ChatResponse {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: text}}
}

func toolStep(id, name, path string) llm.ChatResponse {
	args, _ := json.Marshal(map[string]string{"path": path, "content": "updated"})
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: name, Arguments: args}}}}
}
func writeStep(id, path string) llm.ChatResponse { return toolStep(id, "write_file", path) }
func readStep(id, path string) llm.ChatResponse  { return toolStep(id, "read_file", path) }

func countInjected(history []llm.Message, text string) int {
	n := 0
	for _, m := range history {
		if m.Role == llm.RoleUser && strings.HasPrefix(m.Content, harnessPrefix) && strings.Contains(m.Content, text) {
			n++
		}
	}
	return n
}

type readStub struct {
	name string
	runs *int32
}

func (r readStub) Name() string          { return r.name }
func (readStub) Description() string     { return "read" }
func (readStub) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (readStub) Mode() ToolMode          { return Concurrent }
func (r readStub) Run(context.Context, json.RawMessage) (string, error) {
	atomic.AddInt32(r.runs, 1)
	return "contents", nil
}

type todoFake struct {
	done int
	open []string
}

func (todoFake) Name() string                                         { return "todo" }
func (todoFake) Description() string                                  { return "checklist" }
func (todoFake) Schema() json.RawMessage                              { return json.RawMessage(`{}`) }
func (todoFake) Mode() ToolMode                                       { return Concurrent }
func (todoFake) Run(context.Context, json.RawMessage) (string, error) { return "ok", nil }
func (f todoFake) Progress() (int, []string)                          { return f.done, f.open }

func TestOpenChecklistDoesNotBlockFinish(t *testing.T) {
	c := &stubClient{responses: []llm.ChatResponse{say("need your decision")}}
	a := New(Config{Client: c, Tools: NewRegistry(todoFake{open: []string{"work"}})})
	if out, err := a.Run(t.Context(), "task"); err != nil || out != "need your decision" || c.calls != 1 {
		t.Fatalf("out=%q err=%v calls=%d", out, err, c.calls)
	}
}

func TestRepeatedReadsAndWritesExecuteNormally(t *testing.T) {
	var reads, writes int32
	c := &stubClient{responses: []llm.ChatResponse{readStep("1", "a"), readStep("2", "a"), writeStep("3", "a"), writeStep("4", "a"), say("done")}}
	a := New(Config{Client: c, Tools: NewRegistry(readStub{"read_file", &reads}, countingStub{"write_file", &writes})})
	if _, err := a.Run(t.Context(), "task"); err != nil {
		t.Fatal(err)
	}
	if reads != 2 || writes != 2 || c.calls != 5 {
		t.Fatalf("reads=%d writes=%d calls=%d", reads, writes, c.calls)
	}
}

func TestWorkspaceObservedOnlyAtRunBoundaries(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		observations := 0
		c := &stubClient{responses: []llm.ChatResponse{readStep("1", "a"), readStep("2", "a"), say("done")}}
		a := New(Config{Client: c, Tools: NewRegistry(echoToolStub{"read_file"}), BeginChanges: func(context.Context) (func() ([]string, error), error) {
			return func() ([]string, error) { observations++; return []string{"shell.txt"}, nil }, nil
		}})
		if cancelled {
			a.cfg.OnStep = func(step int, _ llm.Message) {
				if step == 1 {
					cancel()
				}
			}
		}
		_, _ = a.Run(ctx, "task")
		cancel()
		if observations != 1 || len(a.MutatedPaths()) != 1 {
			t.Fatalf("observations=%d report=%+v", observations, a.Report())
		}
	}
}

func TestChecklistCannotExtendStepLimit(t *testing.T) {
	c := &repeatingToolClient{args: json.RawMessage(`{"path":"a"}`)}
	a := New(Config{Client: c, Tools: NewRegistry(mutStub{"write_file"}, todoFake{open: []string{"work"}}), MaxSteps: 3})
	_, err := a.Run(t.Context(), "task")
	if !errors.Is(err, ErrMaxSteps) || c.calls != 3 || a.Report().MaxSteps != 3 {
		t.Fatalf("calls=%d report=%+v err=%v", c.calls, a.Report(), err)
	}
}

func TestFinalVerificationFailureDoesNotCallModelAgain(t *testing.T) {
	c := &stubClient{responses: []llm.ChatResponse{say("done")}}
	a := New(Config{Client: c, Tools: NewRegistry(), Verify: func(context.Context) (string, error) { return "compiler diagnostics", errors.New("exit status 1") }})
	answer, err := a.Run(t.Context(), "task")
	if err == nil || answer != "" || c.calls != 1 || a.Report().Final != "" || a.Report().Verification != "compiler diagnostics" {
		t.Fatalf("answer=%q err=%v report=%+v calls=%d", answer, err, a.Report(), c.calls)
	}
}

func TestAskWithoutGateDenies(t *testing.T) {
	ran := false
	a := New(Config{Tools: NewRegistry(auditTool{&ran}), Policy: permission.Policy{Rules: []permission.Rule{{Tool: "*", Pattern: "*", Effect: permission.Ask}}}})
	a.executeToolCalls(context.Background(), &runState{}, 0, "stop", []llm.ToolCall{{ID: "1", Name: "audit_tool", Arguments: json.RawMessage(`{}`)}}, nil)
	if ran {
		t.Fatal("Ask executed without a gate")
	}
}

type orderedTool struct {
	name  string
	mode  ToolMode
	order *[]string
}

func (t orderedTool) Name() string            { return t.name }
func (t orderedTool) Description() string     { return t.name }
func (t orderedTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t orderedTool) Mode() ToolMode          { return t.mode }
func (t orderedTool) Run(context.Context, json.RawMessage) (string, error) {
	*t.order = append(*t.order, t.name)
	return "ok", nil
}
func TestMutationIsBarrierInModelOrder(t *testing.T) {
	var order []string
	a := New(Config{Tools: NewRegistry(orderedTool{"before", Concurrent, &order}, orderedTool{"write", Exclusive, &order}, orderedTool{"after", Concurrent, &order})})
	var calls []llm.ToolCall
	for _, name := range []string{"before", "write", "after"} {
		calls = append(calls, llm.ToolCall{ID: name, Name: name, Arguments: json.RawMessage(`{}`)})
	}
	a.executeToolCalls(context.Background(), &runState{}, 0, "stop", calls, nil)
	if strings.Join(order, ",") != "before,write,after" {
		t.Fatalf("order=%v", order)
	}
}

func TestAgent_PatchIsNotBlockedByTheRewriteGuard(t *testing.T) {
	for _, tool := range []string{"patch_file", "patch_lines"} {
		t.Run(tool, func(t *testing.T) {
			var runs int32
			client := &stubClient{responses: []llm.ChatResponse{
				patchCall("1", tool, "a.go"),
				patchCall("2", tool, "a.go"),
				patchCall("3", tool, "a.go"),
				{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
			}}
			a := New(Config{
				Client: client,
				Tools:  NewRegistry(countingStub{name: tool, runs: &runs}),
				System: "sys",
			})
			if _, err := a.Run(context.Background(), "task"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := atomic.LoadInt32(&runs); got != 3 {
				t.Fatalf("%s ran %d times, want 3: a patch must never be refused", tool, got)
			}
			for _, m := range client.lastHistory {
				if m.Role == llm.RoleTool && strings.Contains(m.Content, "already wrote") {
					t.Fatalf("%s was refused as a rewrite:\n%s", tool, m.Content)
				}
			}
		})
	}
}

type countingStub struct {
	name string
	runs *int32
}

func (s countingStub) Name() string            { return s.name }
func (s countingStub) Description() string     { return "stub" }
func (s countingStub) Mode() ToolMode          { return Exclusive }
func (s countingStub) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (s countingStub) Run(context.Context, json.RawMessage) (string, error) {
	atomic.AddInt32(s.runs, 1)
	return "ok", nil
}

func patchCall(id, tool, path string) llm.ChatResponse {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
		{ID: id, Name: tool, Arguments: json.RawMessage(`{"path":"` + path + `","content":"x"}`)}}}}
}
