package sessionindex

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/session"
)

func TestScanRecoversMissingAndCorruptSnapshotsAndInvalidatesLogCache(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "recoverable", "state.json")
	history := []llm.Message{{Role: llm.RoleUser, Content: "find this session"}}
	if err := session.Append(path, 1, history); err != nil {
		t.Fatal(err)
	}
	entries, cache := Scan(root, nil, true)
	if len(entries) != 1 || entries[0].Title != "find this session" || entries[0].Msgs != 1 || len(entries[0].Search) != 1 {
		t.Fatalf("missing snapshot recovery: %+v", entries)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	entries, cache = Scan(root, cache, true)
	if len(entries) != 1 || entries[0].Status != "Saved" {
		t.Fatalf("corrupt snapshot recovery: %+v", entries)
	}
	history = append(history, llm.Message{Role: llm.RoleAssistant, Content: "new searchable response"})
	if err := session.Append(path, 2, history); err != nil {
		t.Fatal(err)
	}
	updated := time.Now().Add(time.Hour).Truncate(time.Second)
	if err := os.Chtimes(session.LogPath(path), updated, updated); err != nil {
		t.Fatal(err)
	}
	entries, _ = Scan(root, cache, true)
	if len(entries) != 1 || entries[0].Msgs != 2 || len(entries[0].Search) != 2 || entries[0].Preview != "new searchable response" {
		t.Fatalf("changed recovery log did not invalidate cache: %+v", entries)
	}
	if !entries[0].Updated.Equal(updated) {
		t.Fatal("session age did not follow its updated recovery log")
	}
	if cache[filepath.Dir(path)].Entry.Msgs != 1 {
		t.Fatal("scan mutated previous cache")
	}
}

func TestSearchUsesSourceMessagesAndExcludesPrivateText(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "system secret"},
		{Role: llm.RoleUser, Content: "question" + session.ReferenceStart + "file secret" + session.ReferenceEnd},
		{Role: llm.RoleAssistant, Content: "<think>private secret</think> answer", Reasoning: "reasoning secret"},
		{Role: llm.RoleUser, Content: "[harness] operational secret"},
		{Role: llm.RoleTool, Content: "tool secret"},
	}
	texts := ConversationText(history)
	if len(texts) != 2 || texts[0].Text != "question" || texts[0].Message != 1 || texts[1].Text != "answer" || texts[1].Message != 2 {
		t.Fatalf("index: %+v", texts)
	}
}
