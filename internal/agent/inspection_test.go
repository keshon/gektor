package agent

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
)

func TestInspectionMapsCompactedRequestAndRetainsArchive(t *testing.T) {
	failed := false
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: "task"},
		{Role: llm.RoleUser, Content: "old exact decision"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "skill", Name: "load_skill", Arguments: json.RawMessage(`{"id":"go"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "skill", Content: "full guide", ToolFailed: &failed},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "bad", Name: "broken", InputError: "invalid"}}},
		{Role: llm.RoleUser, Content: "latest"},
	}
	a := New(Config{Tools: NewRegistry()})
	a.snapshot = session.New(history)
	a.snapshot.ContextStart, a.snapshot.Summary = 3, "checkpoint"
	req := llm.ChatRequest{Messages: a.snapshot.Context()}
	req.Messages[4].Content = "guide reuse marker"
	a.requests = 7
	a.recordInspection(req)
	v := a.inspection
	if v.Request.MessageCount != len(llm.ModelMessages(req.Messages)) || !v.Request.HasSummary || v.Summary != "checkpoint" {
		t.Fatal(v)
	}
	if !reflect.DeepEqual(v.Request.Included, []session.MessageRange{{Start: 1, End: 2}, {Start: 4, End: 5}, {Start: 7, End: 7}}) || !reflect.DeepEqual(v.Request.Rewritten, []int{5}) {
		t.Fatal(v.Request)
	}
	if len(v.Skills) != 1 || v.Skills[0].ID != "go" || v.Skills[0].ArchiveID != 5 {
		t.Fatal(v.Skills)
	}
	rows, _, err := session.SearchHistory(a.snapshot, "old exact decision", 1, 1)
	if err != nil || len(rows) != 1 || rows[0].ID != 3 || a.snapshot.History[4].Content != "full guide" {
		t.Fatal(rows, err)
	}
	// A subsequent request must not change evidence held by the previous batch.
	a.requests++
	a.cfg.SystemSections = []llm.PromptSection{{Name: "identity", Text: "changed"}}
	req.Messages[0].Content = "changed"
	a.recordInspection(req)
	if v.Request.Number != 7 || len(v.Prompts) != 1 || v.Prompts[0].Text != "system" {
		t.Fatal("previous view changed", v)
	}
}

func TestInspectionPreservesLegacyPromptOnResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := session.New([]llm.Message{{Role: llm.RoleSystem, Content: "Be be honest"}, {Role: llm.RoleUser, Content: "task"}, {Role: llm.RoleAssistant, Content: "done"}})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	client := &stubClient{responses: []llm.ChatResponse{{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}}}
	cfg := Config{Client: client, Tools: NewRegistry(), StateFile: path, System: "Be honest", SystemSections: []llm.PromptSection{{Name: "identity", Text: "Be honest"}}}
	if _, err := New(cfg).Resume(t.Context(), s.Context(), "compare"); err != nil {
		t.Fatal(err)
	}
	s, err := session.Load(path)
	if err != nil || len(s.Prompts) != 2 || s.Prompts[0].Text != "Be be honest" || s.Prompts[0].Source != "saved_archive" || s.Prompts[1].Text != "Be honest" {
		t.Fatal(s.Prompts, err)
	}
	first := s.LastRequest.Number
	client.calls = 0
	if _, err := New(cfg).Resume(t.Context(), s.Context(), "again"); err != nil {
		t.Fatal(err)
	}
	s, err = session.Load(path)
	if err != nil || len(s.Prompts) != 2 || s.LastRequest.Number <= first {
		t.Fatal(s.LastRequest, s.Prompts, err)
	}
}

func TestInspectionUsesProjectedCallsAndRoleAllowance(t *testing.T) {
	history := []llm.Message{{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: "task"}, {Role: llm.RoleAssistant, Content: "retained text", ToolCalls: []llm.ToolCall{{ID: "bad", Name: "load_skill", InputError: "invalid"}}}}
	a := New(Config{Tools: NewRegistry(), ReadOnly: []string{"inspect_context"}, Directive: "not actually included"})
	a.snapshot, a.requests = session.New(history), 1
	a.recordInspection(llm.ChatRequest{Messages: history, Tools: []llm.ToolDef{{Name: "write_file"}, {Name: "inspect_context"}}})
	v := a.inspection
	if v.Directive != "" || len(v.Request.Rewritten) != 1 || v.Request.Rewritten[0] != 3 || v.Request.MessageCount != 3 {
		t.Fatal(v)
	}
	if len(v.Tools) != 2 || v.Tools[0].Name != "inspect_context" || !v.Tools[0].RoleAllowed || v.Tools[1].RoleAllowed {
		t.Fatal(v.Tools)
	}
}
