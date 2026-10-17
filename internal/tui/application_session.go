package tui

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/session"
)

func (m *model) sessionDirectory() string {
	if m.stateFile == "" {
		return ""
	}
	return filepath.Dir(m.stateFile)
}
func (m *model) saveSessionSettings() error {
	if m.stateFile == "" {
		return nil
	}
	return m.saveSessionSettingsAt(m.sessionDirectory())
}

// saveSessionSettingsAt permits a candidate session to persist its settings
// before the update loop installs it as the current session.
func (m *model) saveSessionSettingsAt(directory string) error {
	if m.application == nil {
		return nil
	}
	p := m.application.Preferences
	if m.config != nil {
		profile := m.config.Profile.Clone()
		p.Profile = &profile
	}
	return config.SavePreferences(filepath.Join(directory, "settings.json"), p)
}
func (m *model) openApplicationSession(entry sessionEntry) (tea.Cmd, bool) {
	path := filepath.Join(entry.dir, "settings.json")
	var p config.Preferences
	var err error
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		// Existing archives predate per-session settings. Keep the current
		// service profile; there is no mode left to restore.
		p = m.application.Preferences
		profile := m.config.Profile.Clone()
		p.Profile = &profile
		if p.Workspace == "" {
			m.sessions.flash = "Choose an Agent workspace in Settings → Application first"
			return nil, true
		}
	} else {
		p, err = config.LoadPreferences(path)
		if err != nil {
			m.sessions.flash = "Cannot read session settings: " + err.Error()
			return nil, true
		}
	}
	p.Theme = m.application.Preferences.Theme
	if p.Profile == nil {
		m.sessions.flash = "Session settings have no profile"
		return nil, true
	}
	history, err := session.ReadHistory(filepath.Join(entry.dir, "state.json"))
	if err != nil {
		m.sessions.flash = fmt.Sprintf("Cannot open session: %v", err)
		return nil, true
	}
	m.saveDraft()
	draft := m.drafts[filepath.Join(entry.dir, "state.json")].text
	m.modeChange = &ModeChange{Preferences: p, History: history, Draft: draft, Directory: entry.dir}
	return tea.Quit, true
}

// Agent installs its current system instruction when resuming. Never carry the
// old session's leading prompt as a second, competing system instruction.
func agentSeed(history []llm.Message) []llm.Message {
	for len(history) > 0 && history[0].Role == llm.RoleSystem {
		history = history[1:]
	}
	return append([]llm.Message{{Role: llm.RoleSystem}}, history...)
}
