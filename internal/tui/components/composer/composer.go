// Package composer provides multiline editing with explicit input-history recall.
package composer

import (
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
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
	history []string
	index   int
	draft   string
}

func New(editor textarea.Model) Model {
	editor.KeyMap.InsertNewline.SetKeys("shift+enter", "ctrl+o")
	return Model{Model: editor}
}
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.Model, cmd = m.Model.Update(msg)
	return m, cmd
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
