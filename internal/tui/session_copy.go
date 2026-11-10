package tui

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

type sessionCopiedMsg struct {
	label, text string
	err         error
}

func (m *model) sessionCopyItems() []menuItem {
	enabled := m.stateFile != ""
	return []menuItem{{"Copy session ID", "copy-session-id", enabled}, {"Copy session path", "copy-session-path", enabled}}
}

func (m *model) sessionCopyValue(command string) (label, text string) {
	if m.stateFile == "" {
		return "", ""
	}
	dir, err := filepath.Abs(filepath.Dir(m.stateFile))
	if err != nil {
		return "", ""
	}
	if command == "copy-session-id" {
		return "Session ID", filepath.Base(dir)
	}
	return "Session path", dir
}

func (m *model) copySession(command string) tea.Cmd {
	label, text := m.sessionCopyValue(command)
	if text == "" {
		return nil
	}
	return copySessionCommand(label, text, clipboard.WriteAll)
}

func copySessionCommand(label, text string, write func(string) error) tea.Cmd {
	return func() tea.Msg {
		return sessionCopiedMsg{label: label, text: text, err: write(text)}
	}
}
