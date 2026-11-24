// Package composer provides multiline editing with explicit input-history recall.
package composer

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/pkg/tui/components/cell"
)

const HistoryLimit = 50

type Snapshot struct {
	History []string
	Index   int
	Draft   string
}

// Model embeds the standard editor so callers can supply their own styles and size.
// Enter is owned by the parent; Shift+Enter and Ctrl+O insert a newline.
type Model struct {
	textarea.Model
	history       []string
	index         int
	draft         string
	CompactPastes bool
	foldedLines   int
}

func New(editor textarea.Model) Model {
	editor.KeyMap.InsertNewline.SetKeys("shift+enter", "ctrl+o")
	return Model{Model: editor}
}
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyPressMsg); ok {
		m.foldedLines = 0
	}
	paste, pasted := msg.(tea.PasteMsg)
	if pasted {
		paste.Content = normalizePaste(paste.Content)
		msg = paste
	}
	var cmd tea.Cmd
	m.Model, cmd = m.Model.Update(msg)
	if pasted && m.Model.Focused() {
		m.foldPaste(paste.Content)
	}
	return m, cmd
}

// Paste keeps the original text in the editor; folding is presentation only.
func (m *Model) Paste(text string) {
	text = normalizePaste(text)
	m.Model.InsertString(text)
	m.foldPaste(text)
}

func normalizePaste(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

func (m *Model) foldPaste(text string) {
	m.foldedLines = 0
	if m.CompactPastes && (strings.Count(text, "\n") >= 2 || len([]rune(text)) > 150) {
		m.foldedLines = strings.Count(m.Model.Value(), "\n") + 1
	}
}

func (m Model) Folded() bool { return m.foldedLines > 0 && m.CompactPastes }
func (m *Model) Unfold()     { m.foldedLines = 0 }
func (m *Model) SetValue(text string) {
	m.foldedLines = 0
	m.Model.SetValue(text)
}
func (m *Model) Reset() {
	m.foldedLines = 0
	m.Model.Reset()
}
func (m Model) Height() int {
	if m.Folded() {
		return 1
	}
	return m.Model.Height()
}
func (m Model) Cursor() *tea.Cursor {
	if m.Folded() {
		return nil
	}
	return m.Model.Cursor()
}
func (m Model) View() string {
	if !m.Folded() {
		return m.Model.View()
	}
	lines := m.foldedLines
	unit := "lines"
	if lines == 1 {
		unit = "line"
	}
	return cell.Line(m.Prompt+fmt.Sprintf("Pasted text (%d %s)", lines, unit), m.Width())
}

func (m *Model) Push(line string) {
	if line == "" {
		return
	}

	if len(m.history) == 0 || m.history[len(m.history)-1] != line {
		m.history = append(m.history, line)
		if len(m.history) > HistoryLimit {
			m.history = m.history[len(m.history)-HistoryLimit:]
		}
	}

	m.index = len(m.history)
}
func (m *Model) Recall(up bool) bool {
	if len(m.history) == 0 {
		return false
	}

	if up {
		if m.index >= len(m.history) {
			m.draft = m.Value()
		}

		m.index = max(m.index-1, 0)
	} else {
		if m.index >= len(m.history) {
			return false
		}

		m.index++
	}

	if m.index == len(m.history) {
		m.SetValue(m.draft)
	} else {
		m.SetValue(m.history[m.index])
	}

	m.CursorEnd()

	return true
}
func (m Model) Snapshot() Snapshot {
	return Snapshot{append([]string(nil), m.history...), m.index, m.draft}
}
func (m *Model) Restore(s Snapshot) {
	m.history = append([]string(nil), s.History...)
	m.index = min(max(s.Index, 0), len(m.history))
	m.draft = s.Draft
}
