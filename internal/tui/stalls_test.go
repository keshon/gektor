package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
	"github.com/keshon/gektor/internal/tui/components/transcript"
)

// savePreferences runs at both ends of every turn and fsyncs. An
// unchanged payload must not reach the disk, or a turn costs two flushes it
// does not need — on the render goroutine.

func TestPreferencesSkipAnUnchangedWrite(t *testing.T) {
	dir := t.TempDir()
	m := sizeModel(t, testModel())
	path := filepath.Join(dir, "application.json")
	m.application = &Application{
		PreferencePath: path,
		Preferences:    config.Preferences{Version: 1},
	}

	if err := m.savePreferences(); err != nil {
		t.Fatal(err)
	}
	first := m.savedSettings
	if first == "" {
		t.Fatal("first write did not record what it stored")
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := m.savePreferences(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an identical payload was written again")
	}

	m.application.Preferences.Theme = "light"
	if err := m.savePreferences(); err != nil {
		t.Fatal(err)
	}
	if m.savedSettings == first {
		t.Fatal("a real change was skipped")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

// CompactFile used to read the snapshot, and then the caller read it again
// to get what it had already decoded.

func TestCompactFileReturnsTheHistoryItWrote(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	var history []llm.Message
	history = append(history,
		llm.Message{Role: llm.RoleSystem, Content: "sys"},
		llm.Message{Role: llm.RoleUser, Content: "task"},
	)
	for i := 0; i < 30; i++ {
		history = append(history,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c", Name: "read_file"}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: "c", Content: "contents"},
		)
	}
	if err := session.WriteSnapshot(state, history); err != nil {
		t.Fatal(err)
	}

	before, after, got, err := agent.CompactFile(state, 8)
	if err != nil {
		t.Fatal(err)
	}
	if after >= before {
		t.Fatalf("nothing compacted: %d -> %d", before, after)
	}
	if len(got) != after {
		t.Fatalf("returned %d messages, compacted to %d", len(got), after)
	}
	// What it returns must be what is on disk, or the transcript and the
	// saved context disagree about what was dropped.
	stored, err := session.ReadContext(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != len(got) {
		t.Fatalf("stored %d messages, returned %d", len(stored), len(got))
	}
}

// The filter matched against strings.ToLower of every cached message on
// every keystroke. The fold belongs at index time.

func TestSearchFoldIsComputedOnceAtIndexTime(t *testing.T) {
	history := []llm.Message{{Role: llm.RoleUser, Content: "Нужный ПАРСЕР работает"}}
	entry := conversationText(history)
	if len(entry) != 1 {
		t.Fatalf("indexed %d messages", len(entry))
	}
	if entry[0].lower == entry[0].Text {
		t.Fatalf("search text was not folded: %q", entry[0].lower)
	}
	if !strings.Contains(entry[0].lower, "парсер") {
		t.Fatalf("fold lost the match: %q", entry[0].lower)
	}
}

// SetParts reassembles the viewport content, which splits every rendered
// line and measures the widest with an ANSI-aware scan. Identical input
// means identical output, so the repeat must be skipped.

func TestSetPartsSkipsAnIdenticalFrame(t *testing.T) {
	m := transcript.New()
	m.Viewport.SetWidth(40)
	m.Viewport.SetHeight(10)
	parts := []string{"alpha", "beta"}
	m.SetParts(parts, "")
	first := m.Viewport.View()

	// A different slice holding equal strings is still an identical frame.
	m.SetParts([]string{"alpha", "beta"}, "")
	if m.Viewport.View() != first {
		t.Fatal("a repeated frame changed the viewport")
	}

	m.SetParts(parts, "tail")
	if !strings.Contains(m.Viewport.View(), "tail") {
		t.Fatal("a real change was skipped")
	}
}
