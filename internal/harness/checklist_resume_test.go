package harness

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

func checklistCall(id, text string) llm.Message {
	args, _ := json.Marshal(map[string]any{"items": []map[string]any{{"id": 1, "state": "pending", "text": text}}}) // fixed JSON-compatible values
	return llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: "todo", Arguments: args}}}
}

func TestResumeChecklistUsesAcceptedUpdates(t *testing.T) {
	tests := []struct {
		name       string
		result     *llm.Message
		inputError string
		want       string
	}{
		{name: "rejected", result: &llm.Message{ToolFailed: new(true), ToolStatus: "rejected"}, want: "accepted work"},
		{name: "failed", result: &llm.Message{ToolFailed: new(true), ToolStatus: "execution_error"}, want: "accepted work"},
		{name: "cancelled", result: &llm.Message{ToolFailed: new(true), ToolStatus: "cancelled"}, want: "accepted work"},
		{name: "withheld", result: &llm.Message{ToolFailed: new(false), ToolStatus: "skipped_for_input"}, want: "accepted work"},
		{name: "unanswered", want: "accepted work"},
		{name: "incomplete stream", inputError: "incomplete_stream", want: "accepted work"},
		{name: "unknown legacy result", result: &llm.Message{}, want: "accepted work"},
		{name: "success", result: &llm.Message{ToolFailed: new(false), ToolStatus: "success"}, want: "new work"},
		{name: "legacy success", result: &llm.Message{ToolFailed: new(false)}, want: "new work"},
		{name: "status only success", result: &llm.Message{ToolStatus: "success"}, want: "new work"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &resumeClient{}
			session, err := New(Config{Env: testEnv(t, client, nil)})
			if err != nil {
				t.Fatal(err)
			}
			history := []llm.Message{
				{Role: llm.RoleSystem, Content: "system"},
				{Role: llm.RoleUser, Content: "finish work"},
				checklistCall("reused-id", "accepted work"),
				{Role: llm.RoleTool, ToolCallID: "reused-id", ToolFailed: new(false), ToolStatus: "success", Content: "accepted"},
			}
			attempt := checklistCall("reused-id", "new work")
			attempt.ToolCalls[0].InputError = tt.inputError
			history = append(history, attempt)
			if tt.result != nil {
				result := *tt.result
				result.Role, result.ToolCallID = llm.RoleTool, "reused-id"
				history = append(history, result)
			}
			_, err = session.Resume(t.Context(), history, "continue")
			if err != nil {
				t.Fatal(err)
			}
			if open := session.Report().OpenTodos; !slices.Equal(open, []string{tt.want}) {
				t.Fatalf("restored checklist = %v, want %q", open, tt.want)
			}
		})
	}
}

func TestResumeChecklistUsesArchiveAfterCompaction(t *testing.T) {
	client := &resumeClient{}
	path := filepath.Join(t.TempDir(), "state.json")
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "finish work"},
		checklistCall("accepted", "accepted work"),
		{Role: llm.RoleTool, ToolCallID: "accepted", ToolFailed: new(false), ToolStatus: "success", Content: "accepted"},
		{Role: llm.RoleAssistant, Content: "more inspection needed"},
	}
	snapshot := conversation.New(history)
	snapshot.ContextStart = 4
	snapshot.Summary = "Earlier work and checklist were compacted."
	err := snapshot.Save(path)
	if err != nil {
		t.Fatal(err)
	}
	history, err = conversation.ReadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := New(Config{Env: testEnv(t, client, nil), StateFile: path})
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.Resume(t.Context(), history, "continue")
	if err != nil {
		t.Fatal(err)
	}
	if open := session.Report().OpenTodos; !slices.Equal(open, []string{"accepted work"}) {
		t.Fatalf("compacted checklist was lost: %v", open)
	}
	for _, message := range client.requests[0].Messages {
		if len(message.ToolCalls) > 0 {
			t.Fatal("restoring the checklist expanded the compact model context")
		}
	}
}

func TestResumeChecklistUsesLastSuccessfulCallWithinGroup(t *testing.T) {
	first, second := checklistCall("first", "old work"), checklistCall("second", "current work")
	first.ToolCalls = append(first.ToolCalls, second.ToolCalls...)
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "finish work"},
		first,
		{Role: llm.RoleTool, ToolCallID: "first", ToolFailed: new(false), ToolStatus: "success"},
		{Role: llm.RoleTool, ToolCallID: "second", ToolFailed: new(false), ToolStatus: "success"},
	}
	session, err := New(Config{Env: testEnv(t, &resumeClient{}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.Resume(t.Context(), history, "continue")
	if err != nil {
		t.Fatal(err)
	}
	if open := session.Report().OpenTodos; !slices.Equal(open, []string{"current work"}) {
		t.Fatalf("restored checklist = %v", open)
	}
}
