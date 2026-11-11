package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

// CompactFile compacts a snapshot in place and reports counts; a
// second call is a no-op that writes nothing.
func TestCompactFile_ShrinksSnapshot(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state.json")
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "task"},
	}
	for i := 0; i < 15; i++ {
		history = append(history, assistantStep("c")...)
	}
	raw, err := json.MarshalIndent(conversation.New(history), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	before, after, _, err := CompactFile(state, 8)
	if err != nil {
		t.Fatalf("CompactFile: %v", err)
	}
	if after >= before {
		t.Fatalf("before=%d after=%d, want shrink", before, after)
	}
	loaded, err := LoadState(state)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(loaded) != after {
		t.Fatalf("snapshot holds %d messages, want %d", len(loaded), after)
	}

	before2, after2, _, err := CompactFile(state, 8)
	if err != nil {
		t.Fatalf("second CompactFile: %v", err)
	}
	if before2 != after || after2 != after {
		t.Fatalf("second call (%d→%d) must be a no-op", before2, after2)
	}
}

func TestCompactFile_MissingSnapshotErrors(t *testing.T) {
	if _, _, _, err := CompactFile(filepath.Join(t.TempDir(), "missing.json"), 8); err == nil {
		t.Fatal("want error for missing snapshot")
	}
}

// /compact reads and rewrites one snapshot. A corrupt file is an error,
// not something to reconstruct: there is no second copy to fall back to,
// and writing a partial reconstruction would look like a recovered
// session.
func TestCompactFile_CorruptSnapshotErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := conversation.WriteSnapshot(path, []llm.Message{{Role: llm.RoleUser, Content: "task"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := CompactFile(path, 8); err == nil {
		t.Fatal("a corrupt snapshot must not compact silently")
	}
}

func TestCompactFile_ShrinksASnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	history := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "task"}}
	for i := 0; i < 15; i++ {
		history = append(history, assistantStep("c")...)
	}
	if err := conversation.WriteSnapshot(path, history); err != nil {
		t.Fatal(err)
	}

	before, after, _, err := CompactFile(path, 8)
	if err != nil || before != len(history) || after >= before {
		t.Fatalf("compaction: %d -> %d, %v", before, after, err)
	}
	loaded, err := LoadState(path)
	if err != nil || len(loaded) != after {
		t.Fatalf("compacted snapshot: %d messages, %v", len(loaded), err)
	}
}
