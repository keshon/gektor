package retrospective

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

func TestScopeLegacyAndTrailingRun(t *testing.T) {
	_, s := archiveFixture(t)
	for _, selection := range []string{"0", "3", "garbage"} {
		if _, _, _, err := scope(s, selection); err == nil {
			t.Fatalf("accepted %q", selection)
		}
	}
	s.History = append(s.History, llm.Message{Role: llm.RoleUser, Content: "unfinished"})
	if _, _, _, err := scope(s, ""); err == nil {
		t.Fatal("ignored unfinished trailing task")
	}
	s.Runs = nil
	if _, _, _, err := scope(s, ""); err == nil {
		t.Fatal("guessed legacy run boundaries")
	}
	start, end, _, err := scope(s, "session")
	if err != nil || start != 1 || end != len(s.History) {
		t.Fatalf("legacy whole session: %d %d %v", start, end, err)
	}
}

func TestArchivePagesUnicodeAndKeepsReasoningAndCallArguments(t *testing.T) {
	text := strings.Repeat("界", 2500)
	s := conversation.New([]llm.Message{{Role: llm.RoleAssistant, Content: text, Reasoning: "reason", ToolCalls: []llm.ToolCall{{ID: "call", Name: "read_file", Arguments: json.RawMessage(`{"path":"original.go"}`)}}}})
	tool := &archiveTool{snapshot: s, seen: map[int]string{}}
	first, err := tool.Run(t.Context(), json.RawMessage(`{"start":1,"count":1}`))
	if err != nil || !utf8.ValidString(first) || !strings.Contains(first, `"next_offset":2000`) {
		t.Fatalf("first page: %v %s", err, first)
	}
	second, err := tool.Run(t.Context(), json.RawMessage(`{"start":1,"count":1,"offset":2000}`))
	if err != nil || !utf8.ValidString(second) || !strings.Contains(second, "reason") || !strings.Contains(second, "original.go") {
		t.Fatalf("second page: %v %s", err, second)
	}
	if !strings.HasPrefix(tool.seen[1], text) {
		t.Fatal("paging lost Unicode characters")
	}
	if tool.covered[1] != len([]rune(messageText(s.History[0]))) {
		t.Fatal("completed pages still reported as partial")
	}
}
