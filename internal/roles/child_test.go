package roles

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
)

func TestDelegatedRunHasSeparateEventsAndSavedHistory(t *testing.T) {
	e := testEnv(t)
	e.Client = &seqStub{steps: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "delegate-1", Name: "delegate_task", Arguments: json.RawMessage(`{"task":"inspect"}`)}}}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "child evidence", Reasoning: "child summary"}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "parent done"}},
	}}
	var parent, child []llm.Message
	e.OnStep = func(_ string, _ int, msg llm.Message) { parent = append(parent, msg) }
	e.OnChild = func(id, kind string, fields map[string]any) {
		if id != "delegate-1" {
			t.Errorf("wrong parent call %q", id)
		}
		if kind == "message" {
			child = append(child, fields["message"].(llm.Message))
		}
	}
	path := filepath.Join(t.TempDir(), "state.json")
	a := Interactive(e, "", path, nil, nil)
	answer, err := a.Run(t.Context(), "delegate this")
	if err != nil || answer != "parent done" {
		t.Fatal(answer, err)
	}
	if len(parent) != 2 || len(child) != 1 || child[0].Content != "child evidence" {
		t.Fatal(parent, child)
	}
	snapshot, err := session.Load(session.ChildStatePath(path, "delegate-1"))
	if err != nil || len(snapshot.History) < 3 {
		t.Fatal(snapshot, err)
	}
	if snapshot.History[1].Content != "inspect" {
		t.Fatal("wrong child task")
	}
	if snapshot.Runs[0].Outcome != "completed" {
		t.Fatal(snapshot.Runs)
	}
}

type streamingChildClient struct{ calls int }

func (c *streamingChildClient) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	return c.Stream(ctx, req, nil, nil)
}
func (c *streamingChildClient) Stream(_ context.Context, _ llm.ChatRequest, reasoning, text func(string)) (llm.ChatResponse, error) {
	c.calls++
	if c.calls == 1 {
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "child-stream", Name: "delegate_task", Arguments: json.RawMessage(`{"task":"inspect"}`)}}}}, nil
	}
	answer := "parent final"
	thought := ""
	if c.calls == 2 {
		answer = "child final"
		thought = "child reasoning"
		if reasoning != nil {
			reasoning(thought)
		}
	}
	if text != nil {
		text(answer)
	}
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: answer, Reasoning: thought}}, nil
}
func TestChildStreamsOnlyToChildObserver(t *testing.T) {
	e := testEnv(t)
	e.Stream = true
	e.Client = &streamingChildClient{}
	parentText, childText, childReason := "", "", ""
	e.OnDelta = func(s string) { parentText += s }
	e.OnReasoning = func(s string) { t.Error("child reasoning leaked", s) }
	e.OnChild = func(id, kind string, fields map[string]any) {
		if kind == "delta" {
			childText += fields["text"].(string)
		}
		if kind == "reasoning" {
			childReason += fields["text"].(string)
		}
	}
	_, err := Interactive(e, "", "", nil, nil).Run(t.Context(), "delegate")
	if err != nil || parentText != "parent final" || childText != "child final" || childReason != "child reasoning" {
		t.Fatal(err, parentText, childText, childReason)
	}
}
