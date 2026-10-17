// Package form owns field editing and navigation, without application settings or persistence.
package form

import (
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

type Field struct {
	Key, Label, Value string
	Multiline         bool
}

type Intent int

const (
	None Intent = iota
	Apply
	Cancel
	Pick
)

// Model owns its fields and editor. The parent supplies field visibility and handles intents.
type Model struct {
	Fields []Field
	Index  int
	Editor textarea.Model
}

func New(fields []Field, editor textarea.Model) Model {
	return Model{Fields: append([]Field(nil), fields...), Index: len(fields), Editor: editor}
}
func (m *Model) Sync() {
	if m.Index >= 0 && m.Index < len(m.Fields) {
		m.Fields[m.Index].Value = m.Editor.Value()
	}
}
func (m *Model) Values() map[string]string {
	m.Sync()
	values := make(map[string]string, len(m.Fields))
	for _, f := range m.Fields {
		values[f.Key] = f.Value
	}
	return values
}
func (m *Model) Focus(index int, visible func(int) bool) {
	m.Sync()
	count := len(m.Fields) + 2
	step := 1
	if index < m.Index {
		step = -1
	}
	m.Index = (index%count + count) % count
	// Action buttons are always reachable, even if every field is hidden.
	for m.Index < len(m.Fields) && visible != nil && !visible(m.Index) {
		m.Index = (m.Index + step + count) % count
	}
	m.Editor.Blur()
	if m.Index < len(m.Fields) {
		m.Editor.SetValue(m.Fields[m.Index].Value)
		m.Editor.Focus()
	}
}
func (m Model) View() string { return m.Editor.View() }

// Update receives input only while the parent gives this form focus.
func (m *Model) Update(msg tea.Msg, visible func(int) bool, choice bool, destructive bool) (Intent, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			return Cancel, nil
		case "ctrl+s":
			if !destructive || m.Index == len(m.Fields) {
				return Apply, nil
			}
			return None, nil
		case "space":
			if choice {
				return Pick, nil
			}
		case "left", "right":
			if m.Index >= len(m.Fields) {
				step := 1
				if key.String() == "left" {
					step = -1
				}
				m.Focus(len(m.Fields)+(m.Index-len(m.Fields)+step+2)%2, visible)
				return None, nil
			}
		case "up", "down":
			if m.Index >= len(m.Fields) || !m.Fields[m.Index].Multiline {
				step := 1
				if key.String() == "up" {
					step = -1
				}
				m.Focus(m.Index+step, visible)
				return None, nil
			}
		case "ctrl+up", "shift+tab":
			m.Focus(m.Index-1, visible)
			return None, nil
		case "ctrl+down", "tab":
			m.Focus(m.Index+1, visible)
			return None, nil
		case "enter":
			if choice {
				return Pick, nil
			}
			if m.Index == len(m.Fields) {
				return Apply, nil
			}
			if m.Index == len(m.Fields)+1 {
				return Cancel, nil
			}
			m.Focus(m.Index+1, visible)
			return None, nil
		}
	}
	if m.Index < len(m.Fields) {
		var cmd tea.Cmd
		m.Editor, cmd = m.Editor.Update(msg)
		return None, cmd
	}
	return None, nil
}
