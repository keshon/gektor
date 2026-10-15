package composer

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

func TestMultilineArrowsDoNotRecallAndExplicitRecallRestoresDraft(t *testing.T) {
	m := New(textarea.New())
	m.Focus()
	m.Push("previous")
	m.SetValue("draft\nsecond")
	m.CursorEnd()
	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_ = cmd
	if m.Value() != "draft\nsecond" {
		t.Fatal("arrow recalled history")
	}
	if !m.Recall(true) || m.Value() != "previous" {
		t.Fatal("explicit recall missing")
	}
	if !m.Recall(false) || m.Value() != "draft\nsecond" {
		t.Fatal("multiline draft lost")
	}
	snapshot := m.Snapshot()
	snapshot.History[0] = "changed"
	if m.Snapshot().History[0] != "previous" {
		t.Fatal("snapshot aliases history")
	}
}
func TestCompletionUsesRuneRangesAndKeepsSurroundingLines(t *testing.T) {
	m := New(textarea.New())
	m.SetValue("Привет\n@file tail")
	m.CursorDown()
	m.SetCursorColumn(5)
	start, end, prefix := m.Token()
	if prefix != "@file" {
		t.Fatalf("token: %q", prefix)
	}
	m.Replace(start, end, "@folder/", 0)
	if m.Value() != "Привет\n@folder/ tail" || m.Line() != 1 || m.Column() != 8 {
		t.Fatalf("replacement: %q %d:%d", m.Value(), m.Line(), m.Column())
	}
}
func TestHistoryIsBoundedAndDeduplicatesAdjacentEntries(t *testing.T) {
	m := New(textarea.New())
	for i := 0; i < 60; i++ {
		m.Push(string(rune('a' + i)))
	}
	last := m.Snapshot().History[HistoryLimit-1]
	m.Push(last)
	if len(m.Snapshot().History) != HistoryLimit {
		t.Fatal("history is unbounded or duplicate stored")
	}
}
