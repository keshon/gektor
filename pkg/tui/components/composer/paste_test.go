package composer

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

func TestFoldedPasteKeepsOriginalAndUnfoldsForEditing(t *testing.T) {
	editor := textarea.New()
	editor.DynamicHeight = true
	editor.SetWidth(60)
	editor.Focus()
	m := New(editor)
	m.CompactPastes = true
	m.SetValue("Review this:\n")
	m.CursorEnd()
	pasted := strings.Repeat("a full original line\n", 42)
	m.Paste(pasted)
	original := m.Value()
	if original != "Review this:\n"+pasted || !m.Folded() || m.Height() != 1 || m.Cursor() != nil || !strings.Contains(m.View(), "Pasted text") {
		t.Fatal("paste was replaced or did not fold")
	}
	m.Unfold()
	if m.Folded() || m.Value() != original || strings.Contains(m.View(), "Pasted text") {
		t.Fatal("unfolding changed original text")
	}
	m.Paste(pasted)
	original = m.Value()
	m, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.Folded() || m.Value() != original+"x" {
		t.Fatal("typing into folded draft lost text")
	}
	m.Paste(pasted)
	m.SetValue("new draft")
	if m.Folded() || m.Value() != "new draft" {
		t.Fatal("fold state leaked into a replacement draft")
	}
}

func TestCompactPasteNormalizesWindowsLineEndings(t *testing.T) {
	editor := textarea.New()
	editor.Focus()
	m := New(editor)
	m.CompactPastes = true
	for _, text := range []string{"one\rtwo\rthree", "one\r\ntwo\r\nthree"} {
		m.Reset()
		m, _ = m.Update(tea.PasteMsg{Content: text})
		if m.Value() != "one\ntwo\nthree" || !m.Folded() {
			t.Fatal("Windows paste lost its line boundaries", m.Value())
		}
	}
}
