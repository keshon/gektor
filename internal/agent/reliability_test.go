package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/session"
)

func TestAllPathResourcesAreAuthorizedBeforeMutation(t *testing.T) {
	runs := 0
	c := &repeatSeqClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "move", Name: "required_tool", Arguments: json.RawMessage(`{"path":"source"}`)}}}}, say("blocked"),
	}}
	a := New(Config{Client: c, Tools: NewRegistry(requiredTool{&runs}), System: "sys",
		Resources: func(string, json.RawMessage) ([]string, error) { return []string{"source", "protected"}, nil },
		Policy:    permission.Policy{Rules: []permission.Rule{{Tool: "required_tool", Pattern: "protected", Effect: permission.Deny}}},
	})
	if _, err := a.Run(t.Context(), "task"); err != nil {
		t.Fatal(err)
	}
	if runs != 0 {
		t.Fatal("destination denial was ignored")
	}
}

type requiredTool struct{ runs *int }

func (requiredTool) Name() string        { return "required_tool" }
func (requiredTool) Description() string { return "test mutation" }
func (requiredTool) Mode() ToolMode      { return Exclusive }
func (requiredTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"count":{"type":"integer","minimum":1}},"required":["path"],"additionalProperties":false}`)
}
func (t requiredTool) Run(context.Context, json.RawMessage) (string, error) {
	*t.runs++
	return "ok", nil
}

func TestRegistryRejectsInvalidInputsBeforeMutation(t *testing.T) {
	runs := 0
	registry := NewRegistry(requiredTool{&runs})
	for _, args := range []string{`{}`, `null`, `[]`, `{"path":4}`, `{"path":""}`, `{"path":"a","count":1.5}`, `{"path":"a","extra":1}`, `{"path":"cut`} {
		if _, err := registry.Run(t.Context(), "required_tool", json.RawMessage(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	if runs != 0 {
		t.Fatal("invalid input executed")
	}
	if _, err := registry.Run(t.Context(), "required_tool", json.RawMessage(`{"path":"a","count":2}`)); err != nil || runs != 1 {
		t.Fatal(err, runs)
	}
}

func TestRejectedRawInputSurvivesWithoutFakeWireCall(t *testing.T) {
	const raw = `{"path":"notes.go","content":"unfinished`
	runs := 0
	c := &repeatSeqClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "bad", Name: "required_tool", Arguments: json.RawMessage(raw)}, {ID: "good", Name: "required_tool", Arguments: json.RawMessage(`{"path":"ok"}`)}}}, FinishReason: "length"}, say("done"),
	}}
	path := filepath.Join(t.TempDir(), "state.json")
	var delivered []llm.Message
	a := New(Config{Client: c, Tools: NewRegistry(requiredTool{&runs}), System: "sys", StateFile: path, OnContext: func(m llm.Message) { delivered = append(delivered, m) }})
	if _, err := a.Run(t.Context(), "task"); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatal("valid sibling was lost or invalid input executed", runs)
	}
	for _, m := range c.reqs[1].Messages {
		for _, call := range m.ToolCalls {
			if call.ID == "bad" {
				t.Fatal("rejected call replayed to model")
			}
		}
		if m.ToolCallID == "bad" {
			t.Fatal("orphan tool result")
		}
	}
	if len(delivered) != 1 || delivered[0].Request != 2 || !strings.Contains(delivered[0].Content, raw) || !strings.Contains(delivered[0].Content, "not omitted") {
		t.Fatalf("wrong diagnostic delivery: %+v", delivered)
	}
	s, err := session.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	call := s.History[2].ToolCalls[0]
	if call.RawArguments != raw || call.InputError == "" || string(call.Arguments) != "null" {
		t.Fatalf("raw input lost: %+v", call)
	}
	found := false
	for _, m := range s.History {
		if m.Harness == "tool_input" {
			found = m.Request == 2
		}
	}
	if !found {
		t.Fatal("delivery metadata not persisted")
	}
}

func TestCompactRetainsEveryMultilineUserInstructionAcrossPasses(t *testing.T) {
	h := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "task"}}
	var wants []string
	for i := 0; i < 12; i++ {
		text := fmt.Sprintf("request %d; preserve punctuation\nDO NOT touch private-%d", i, i)
		wants = append(wants, text)
		h = append(h, llm.Message{Role: llm.RoleAssistant, Content: "working"}, llm.Message{Role: llm.RoleUser, Content: text})
	}
	h = append(h, llm.Message{Role: llm.RoleAssistant, Content: "tail"})
	h = compactHistory(h, 1)
	h = append(h, llm.Message{Role: llm.RoleAssistant, Content: "new tail"})
	h = compactHistory(h, 1)
	facts, ok := parseFacts(strings.TrimPrefix(h[2].Content, harnessPrefix))
	if !ok || !reflect.DeepEqual(facts.asks, wants) {
		t.Fatalf("instructions lost: %#v", facts.asks)
	}
}

func TestScopedRulesArriveBeforeMutation(t *testing.T) {
	runs := 0
	c := &repeatSeqClient{responses: []llm.ChatResponse{
		stepOf(llm.ToolCall{ID: "first", Name: "required_tool", Arguments: json.RawMessage(`{"path":"nested/a.go"}`)}),
		stepOf(llm.ToolCall{ID: "retry", Name: "required_tool", Arguments: json.RawMessage(`{"path":"nested/a.go"}`)}), say("done"),
	}}
	a := New(Config{Client: c, Tools: NewRegistry(requiredTool{&runs}), Instructions: func(json.RawMessage) (string, error) { return "Applies only to nested/: preserve exports", nil }, OnStep: func(step int, _ llm.Message) {
		if step == 1 && runs != 0 {
			t.Fatal("mutation happened before model received scoped rules")
		}
	}})
	if _, err := a.Run(t.Context(), "task"); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || !strings.Contains(c.reqs[1].Messages[len(c.reqs[1].Messages)-1].Content, "preserve exports") {
		t.Fatal("scoped delivery failed", runs)
	}
}

func TestCheckpointFailureLeavesPreviousProjectionUntouched(t *testing.T) {
	h := longHistory(12, 200)
	original := append([]llm.Message(nil), h...)
	c := &repeatSeqClient{responses: []llm.ChatResponse{say(`{"goal":"cut`)}}
	a := New(Config{Client: c, Tools: NewRegistry(), Summarize: true, ContextLimit: 4096, CompactKeepSteps: 2})
	st := &runState{}
	if a.maybeCompact(&h, llm.Usage{PromptTokens: 4000}, st, t.Context()) || st.compactErr == nil || !reflect.DeepEqual(h, original) {
		t.Fatal("failed checkpoint committed a partial projection")
	}
}

func TestCheckpointKeepsVerbatimRequirements(t *testing.T) {
	h := longHistory(12, 200)
	h[3].Content = "Keep А; Б\nDo not publish"
	c := &repeatSeqClient{responses: []llm.ChatResponse{say(`{"goal":"repair","decisions":[],"completed":[],"remaining":["verify"],"evidence":[]}`)}}
	a := New(Config{Client: c, Tools: NewRegistry(), Summarize: true, ContextLimit: 4096, CompactKeepSteps: 2})
	if !a.maybeCompact(&h, llm.Usage{PromptTokens: 4000}, &runState{}, t.Context()) {
		t.Fatal("no checkpoint")
	}
	facts, _ := parseFacts(strings.TrimPrefix(h[2].Content, harnessPrefix))
	if !containsStr(facts.asks, "Keep А; Б\nDo not publish") || !strings.Contains(facts.checkpoint, "verify") || len(c.reqs[0].Tools) != 0 {
		t.Fatal("requirements or checkpoint lost", facts)
	}
}
