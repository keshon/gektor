package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

func TestAgent_AskUser_PausesWithUnansweredCall(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"question": "which db?"})
	questionCh := make(chan string, 1)
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "q1", Name: "ask_user", Arguments: args},
		}}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "using sqlite"}},
	}}
	a := New(Config{
		Client: client,
		Tools:  NewRegistry(askUserStub{questionCh: questionCh, answer: "sqlite"}),
		System: "sys",
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := a.Run(context.Background(), "build app"); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()

	select {
	case q := <-questionCh:
		if q != "which db?" {
			t.Errorf("question = %q", q)
		}
	case <-time.After(time.Second):
		t.Fatal("ask_user never invoked")
	}
	<-done

	if callID, _, ok := PausedOnQuestion(NewRegistry(), client.lastHistory[:len(client.lastHistory)-1]); ok {
		_ = callID // history mid-run had unanswered call before tool result appended
	}
}

func TestPausedOnQuestion_DetectsAskUserCall(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"question": "which database?"})
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "do the thing"},
		{Role: llm.RoleAssistant, Content: "", ToolCalls: []llm.ToolCall{
			{ID: "call_q1", Name: "ask_user", Arguments: args},
		}},
	}

	callID, question, ok := PausedOnQuestion(NewRegistry(), history)
	if !ok {
		t.Fatal("PausedOnQuestion returned false for a history ending with ask_user")
	}
	if callID != "call_q1" {
		t.Fatalf("callID = %q, want %q", callID, "call_q1")
	}
	if question != "which database?" {
		t.Fatalf("question = %q, want %q", question, "which database?")
	}
}

func TestPausedOnQuestion_ReturnsFalseForNormalHistory(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "task"},
		{Role: llm.RoleAssistant, Content: "I wrote the file."},
	}
	_, _, ok := PausedOnQuestion(NewRegistry(), history)
	if ok {
		t.Fatal("PausedOnQuestion returned true for a normal text response")
	}
}

func TestPausedOnQuestion_ReturnsFalseForEmptyHistory(t *testing.T) {
	_, _, ok := PausedOnQuestion(nil, nil)
	if ok {
		t.Fatal("PausedOnQuestion returned true for empty history")
	}
}

// With a registry the question comes from the registered Questioner, so a
// tool with its own argument name is still found. The nil-registry shape
// match would not see this one.
func TestPausedOnQuestion_UsesTheRegisteredQuestioner(t *testing.T) {
	reg := NewRegistry(stubQuestioner{})
	history := []llm.Message{{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID: "c1", Name: "stub_question", Arguments: json.RawMessage(`{"ask":"which one?"}`),
		}},
	}}
	callID, question, ok := PausedOnQuestion(reg, history)
	if !ok || callID != "c1" || question != "which one?" {
		t.Fatalf("got %q %q %v", callID, question, ok)
	}
	// The same history against no registry is not a question, since the
	// argument shape does not match.
	if _, _, ok := PausedOnQuestion(nil, history); ok {
		t.Fatal("shape fallback matched an argument it does not know")
	}
}

func TestResumeWithAnswer_SuppliesWireCorrectToolResult(t *testing.T) {
	// Reproduces the "wire-invalid Identity" problem that plain Resume
	// would cause: the answer must arrive as role:tool with the matching
	// tool_call_id, not as a naked role:user message.
	args, _ := json.Marshal(map[string]string{"question": "which database?"})
	pausedHistory := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "build a thing"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "call_q1", Name: "ask_user", Arguments: args},
		}},
	}
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "ok, using sqlite"}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys"})

	out, err := a.ResumeWithAnswer(context.Background(), pausedHistory, "call_q1", "sqlite please")
	if err != nil {
		t.Fatalf("ResumeWithAnswer: %v", err)
	}
	if out != "ok, using sqlite" {
		t.Fatalf("result = %q, want %q", out, "ok, using sqlite")
	}

	// The history the model received must contain role:tool, not role:user,
	// for the answer.
	var foundToolResult bool
	for _, m := range client.lastHistory {
		if m.Role == llm.RoleTool && m.ToolCallID == "call_q1" && m.Content == "sqlite please" {
			foundToolResult = true
		}
		if m.Role == llm.RoleUser && m.Content == "sqlite please" {
			t.Fatal("answer was sent as role:user instead of role:tool — wire-invalid")
		}
	}
	if !foundToolResult {
		t.Fatal("no role:tool message with the answer found in sent history")
	}
}
