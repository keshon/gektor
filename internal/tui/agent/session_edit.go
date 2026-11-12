package agent

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/screen"
)

func (m *Model) formKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	return m, m.chrome.FormKey(msg)
}

func (m *Model) renameActive() {
	dir, title := filepath.Dir(m.stateFile), m.activeSessionTitle()
	m.openForm(screen.Form{
		Title:      "Rename session",
		Hint:       "Changes the title only; history stays intact.",
		ApplyLabel: "Rename",
		Fields:     []form.Field{{Key: "title", Label: "Session title", Value: title, Multiline: false}},
	}, func(values map[string]string) error {
		title := conversation.TitleLine(values["title"])
		if title == "" {
			return fmt.Errorf("title cannot be blank")
		}
		writeErr := conversation.WriteSessionTitle(dir, title)
		if writeErr != nil {
			return writeErr
		}
		m.updateSessionTitle(dir, title)
		m.refreshSessions()
		return nil
	})
}

func (m *Model) deleteActive() {
	target, valid := sessionDeleteTarget(m.sessionRoot(), filepath.Dir(m.stateFile))
	if !valid {
		m.chrome.Open("Cannot delete", []string{"Session is outside the configured session store."})
		return
	}
	m.openForm(screen.Form{
		Title:      "Delete session",
		Hint:       "Permanently removes this saved session.",
		ApplyLabel: "Delete",
		Fields:     nil,
	}, func(_ map[string]string) error {
		err := os.RemoveAll(target)
		if err != nil {
			return err
		}
		m.newTask()
		m.refreshSessions()
		return nil
	})
	m.chrome.Dialog.Form.Destructive = true
	// Destructive confirmation starts on Cancel.
	m.chrome.FocusForm(1)
}

func (m *Model) inspectConnection() {
	m.chrome.Open("Model connection", []string{"Model: " + nonEmpty(m.modelName), "Protocol: " + nonEmpty(m.backendKind), "Endpoint: " + nonEmpty(m.backendURL), fmt.Sprintf("Context: %d tokens · response: %d tokens", m.limit, m.responseCap), "Coding connection changes currently require restarting the application."})
}

func (m *Model) inspectAgentInstructions() {
	if m.config == nil {
		return
	}
	s := m.config.Identity
	selected := ""
	if m.config.Profile.Identity != nil {
		selected = m.config.Profile.Identity.PromptPreset
	}
	preset, _ := prompts.ResponsePreset(selected)
	lines := []string{
		"Prompt preset: " + preset.Name,
		"Preset adds response style to the profile's own identity.",
		"Leading identity instruction",
		s.System,
		"Post-history system directive",
		s.PostHistory,
		"This is the identity the profile contributes; the agent's Plan/Act operational instructions are added on top and are not shown here.",
	}
	lines = append(lines, m.config.Warnings...)
	m.chrome.Open("Agent instructions preview", lines)
}

func (m *Model) confirmCompact() {
	stateFile := m.stateFile
	m.openForm(screen.Form{
		Title:      "Compact history",
		Hint:       "Replaces older tool details with a short record and keeps recent steps. This changes the saved history used by the model.",
		ApplyLabel: "Compact",
		Fields:     nil,
	}, func(_ map[string]string) error {
		if m.stateFile != stateFile {
			return fmt.Errorf("session changed; reopen compact history")
		}

		_, cmd := m.compactNow()
		if cmd != nil {
			m.pendingCommands = append(m.pendingCommands, cmd)
		}
		return nil
	})
	m.chrome.FocusForm(1)
}
