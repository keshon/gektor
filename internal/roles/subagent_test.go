package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
)

// A subagent that could delegate could spawn subagents without bound, and
// one that could ask would block the process on a human watching the
// parent.
func TestSubagent_CannotDelegateOrAsk(t *testing.T) {
	a, aErr := Subagent(testEnv(t), "reviewer")
	if aErr != nil {
		t.Fatal(aErr)
	}
	for _, forbidden := range []string{"delegate_task", "ask_user"} {
		if hasTool(a, forbidden) {
			t.Errorf("subagent can call %s — got %v", forbidden, a.ToolNames())
		}
	}
	if !hasTool(a, "write_file") {
		t.Error("subagent should still have the working tool set")
	}
}

// A subagent's blow-by-blow must not leak into parent evidence: results,
// usage, and nudges stay inside the subagent even when they fire (two
// failing reads earn a stuck nudge here). The parent learns the outcome
// from the DELEGATE envelope, never the internals.
func TestSubagent_ObserverHooksStaySilent(t *testing.T) {
	var toolResults, usages, nudges int32
	e := testEnv(t)
	e.OnResult = func(agent.ToolResult) { atomic.AddInt32(&toolResults, 1) }
	e.OnUsage = func(int, llm.Usage) { atomic.AddInt32(&usages, 1) }
	e.OnNudge = func(string, string) { atomic.AddInt32(&nudges, 1) }
	e.OnStep = func(string, int, llm.Message) { t.Error("child step reached parent") }
	e.OnToolFinished = func(string) { t.Error("child tool completion reached parent") }
	e.OnToolOutput = func(string, string) { t.Error("child tool output reached parent") }
	e.OnPrompt = func(llm.ContextReport) { t.Error("child prompt reached parent") }
	e.OnProgress = func(llm.Progress) { t.Error("child progress reached parent") }
	e.OnContext = func(llm.Message) { t.Error("child context reached parent") }
	e.OnLocal = func(string) { t.Error("child notice reached parent") }
	e.Stream = true
	e.OnDelta = func(string) { t.Error("child text reached parent") }
	e.OnReasoning = func(string) { t.Error("child reasoning reached parent") }
	client := &seqStub{steps: []llm.ChatResponse{
		missingRead("c1"),
		missingRead("c2"),
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	e.Client = client
	sub, subErr := Subagent(e, "")
	if subErr != nil {
		t.Fatal(subErr)
	}

	out, err := sub.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "done" {
		t.Fatalf("out = %q", out)
	}
	if toolResults != 0 || usages != 0 || nudges != 0 {
		t.Fatalf("subagent leaked observers: results=%d usage=%d nudges=%d",
			toolResults, usages, nudges)
	}
}

func TestSubagentKeepsOwnBudgetAndDoesNotConsumeParentSteering(t *testing.T) {
	env := testEnv(t)
	client := &subagentBudgetClient{}
	env.Client = client
	env.MaxSteps = 999999
	env.Inbox = &agent.Inbox{}
	if err := env.Inbox.Send("parent-only steering"); err != nil {
		t.Fatal(err)
	}
	env.OnInput = func(llm.Message) { t.Error("subagent inherited parent input hook") }
	sub, subErr := Subagent(env, "")
	if subErr != nil {
		t.Fatal(subErr)
	}
	if _, err := sub.Run(t.Context(), "subtask"); err != nil {
		t.Fatal(err)
	}
	if sub.Report().MaxSteps != 12 || env.Inbox.Len() != 1 {
		t.Fatalf("budget=%d pending=%d", sub.Report().MaxSteps, env.Inbox.Len())
	}
	if !strings.Contains(client.history[0].Content, "Your budget is 12 model steps") {
		t.Fatal("subagent cannot see its budget")
	}
	for _, msg := range client.history {
		if strings.Contains(msg.Content, "parent-only steering") {
			t.Fatal("subagent consumed parent inbox")
		}
	}
}

func TestDelegateEnforcesRequestedBudgetAndWarnsBeforeExhaustion(t *testing.T) {
	env := testEnv(t)
	env.DelegateSteps, env.DelegateMaxSteps = 3, 6
	calls := 0
	warnings := 0
	env.ChildEvents = childEventVisitor(func(id, kind string, fields map[string]any) {
		if msg, ok := fields["message"].(llm.Message); ok && strings.Contains(msg.Content, "2 model steps remain") {
			warnings++
		}
	})
	var result agent.ToolResult
	var inspected agent.ToolResult
	env.OnResult = func(r agent.ToolResult) {
		if r.CallID == "child" {
			result = r
		}
		if r.CallID == "inspect" {
			inspected = r
		}
	}
	env.Client = roleClientFunc(func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		calls++
		if calls == 1 {
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "child", Name: "delegate_task", Arguments: json.RawMessage(`{"task":"inspect","max_steps":4}`)}}}}, nil
		}
		if calls <= 5 {
			if !strings.Contains(req.Messages[0].Content, "Your budget is 4 model steps") {
				t.Fatal("requested budget not applied")
			}
			args, _ := json.Marshal(map[string]string{"path": fmt.Sprintf("missing-%d", calls)})
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: fmt.Sprint(calls), Name: "read_file", Arguments: args}}}}, nil
		}
		if calls == 6 {
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "inspect", Name: "read_history", Arguments: json.RawMessage(`{"delegate_call_id":"child","query":"missing-"}`)}}}}, nil
		}
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "Child stopped; work is incomplete."}}, nil
	})
	interactiveAgent, interactiveAgentErr := Interactive(env, "", filepath.Join(t.TempDir(), "state.json"), nil, nil)
	if interactiveAgentErr != nil {
		t.Fatal(interactiveAgentErr)
	}
	if _, err := interactiveAgent.Run(t.Context(), "test"); err != nil {
		t.Fatal(err)
	}
	if calls != 7 || warnings != 1 || !result.Failed || !strings.Contains(result.Output, "steps: 4/4") || !strings.Contains(result.Output, "termination: step_limit") || !strings.Contains(result.Output, "archive:") || !strings.Contains(result.Output, "delegate_call_id: child") {
		t.Fatalf("calls=%d warnings=%d result=%+v", calls, warnings, result)
	}
	if inspected.Failed || !strings.Contains(inspected.Output, "missing-") {
		t.Fatalf("parent could not inspect partial results: %+v", inspected)
	}
}
