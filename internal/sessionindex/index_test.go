package sessionindex

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/session"
)

func TestScanIndexesSnapshotAndInvalidatesOnChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "live", "state.json")
	history := []llm.Message{{Role: llm.RoleUser, Content: "find this session"}}
	if err := session.WriteSnapshot(path, history); err != nil {
		t.Fatal(err)
	}

	entries, cache := Scan(root, nil, true)
	if len(entries) != 1 || entries[0].Title != "find this session" || entries[0].Msgs != 1 || len(entries[0].Search) != 1 {
		t.Fatalf("indexed wrong: %+v", entries)
	}

	history = append(history, llm.Message{Role: llm.RoleAssistant, Content: "new searchable response"})
	if err := session.WriteSnapshot(path, history); err != nil {
		t.Fatal(err)
	}
	updated := time.Now().Add(time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, updated, updated); err != nil {
		t.Fatal(err)
	}

	entries, _ = Scan(root, cache, true)
	if len(entries) != 1 || entries[0].Msgs != 2 || len(entries[0].Search) != 2 || entries[0].Preview != "new searchable response" {
		t.Fatalf("a changed snapshot must invalidate the cache: %+v", entries)
	}
	if !entries[0].Updated.Equal(updated) {
		t.Fatal("session age must follow its snapshot")
	}
	if cache[filepath.Dir(path)].Entry.Msgs != 1 {
		t.Fatal("scan mutated previous cache")
	}
}

// A snapshot that no longer decodes is reported as unreadable. It used to
// be silently recovered from the adjacent log, which meant the session
// list showed stale text for a file that had just been replaced.
func TestScanReportsCorruptSnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "broken", "state.json")
	if err := session.WriteSnapshot(path, []llm.Message{{Role: llm.RoleUser, Content: "was fine"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}

	entries, _ := Scan(root, nil, false)
	if len(entries) != 1 || entries[0].Status != "Unreadable" {
		t.Fatalf("corrupt snapshot must surface, not be papered over: %+v", entries)
	}
}