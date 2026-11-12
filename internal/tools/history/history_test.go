package history

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

func TestReadHistorySearchAndOriginalPaging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := conversation.New([]llm.Message{{Role: llm.RoleUser, Content: "task"}, {Role: llm.RoleTool, Content: "verification FAILED"}, {Role: llm.RoleAssistant, Content: "failed to finish"}})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	tool := Tool{Path: path}
	for _, input := range []string{`{"query":"failed","count":1}`, `{"start":2,"count":1}`, `{}`} {
		out, err := tool.Run(t.Context(), json.RawMessage(input))
		if err != nil || !json.Valid([]byte(out)) {
			t.Fatal(out, err)
		}
	}
	out, err := tool.Run(t.Context(), json.RawMessage(`{"query":"failed","count":1}`))
	if !strings.Contains(out, `"source":"saved_archive"`) {
		t.Fatal("archive source missing", out)
	}
	var result struct {
		Next     int                       `json:"next_start"`
		Messages []conversation.HistoryRow `json:"messages"`
	}
	if json.Unmarshal([]byte(out), &result) != nil || err != nil || result.Next != 3 || result.Messages[0].ID != 2 {
		t.Fatal(out, err)
	}
	if _, err := tool.Run(t.Context(), json.RawMessage(`{"query":"failed","offset":1}`)); err == nil {
		t.Fatal("accepted query with offset")
	}
}

func TestReadHistorySelectsOnlyOwnDelegateArchive(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "state.json")
	child := conversation.ChildStatePath(parent, "call-1")
	if err := conversation.New([]llm.Message{{Role: llm.RoleTool, ToolCallID: "check", Content: "exit_code: 1; actual failure"}}).Save(child); err != nil {
		t.Fatal(err)
	}
	tool := Tool{Path: parent}
	out, err := tool.Run(t.Context(), json.RawMessage(`{"delegate_call_id":"call-1","query":"actual failure"}`))
	if err != nil || !strings.Contains(out, "exit_code: 1") {
		t.Fatal(out, err)
	}
	for _, args := range []string{`{"delegate_call_id":"../state.json"}`, `{"delegate_call_id":"call-1","query":"failure","offset":1}`} {
		if out, err := tool.Run(t.Context(), json.RawMessage(args)); err == nil {
			t.Fatalf("unexpected archive access: %s", out)
		}
	}
	other := Tool{Path: filepath.Join(dir, "other.json")}
	if _, err := other.Run(t.Context(), json.RawMessage(`{"delegate_call_id":"call-1"}`)); err == nil {
		t.Fatal("read another parent's child")
	}
}
