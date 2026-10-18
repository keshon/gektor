package tui

import (
	"encoding/json"
	"fmt"
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

// savePreferences writes the one set of application preferences.
//
// There used to be a second copy in every saved conversation's directory.
// Opening an old conversation then restored the backend, model and workspace
// it was created with, which read as the settings belonging to it - so
// changing a backend appeared to take effect for one conversation and
// silently not for the next one you opened. Preferences describe the
// installation, not a transcript, and there is one of them now.
//
// An unchanged write is skipped. This runs at both ends of every turn and
// the payload is a few kilobytes that rarely differ, while SavePreferences
// fsyncs - FlushFileBuffers, tens of milliseconds on a spinning disk or an
// AV-filtered path - on the render goroutine. Skipping the redundant write
// costs nothing and keeps durability for the writes that carry a change.
func (m *model) savePreferences() error {
	if m.application == nil || m.application.Temporary {
		return nil
	}
	p := m.application.Preferences
	if m.config != nil {
		profile := m.config.Profile.Clone()
		p.Profile = &profile
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if m.savedSettings == string(data) {
		return nil
	}
	if err := config.SavePreferences(m.application.PreferencePath, p); err != nil {
		return err
	}
	m.savedSettings = string(data)
	return nil
}

// openApplicationSession opens a saved conversation.
//
// Its settings are not restored: preferences are global and already loaded.
// Existing conversations still carry a settings.json from when they were
// per-conversation; those files are left exactly where they are and simply
// not read, so nothing is destroyed if one turns out to matter.
func (m *model) openApplicationSession(entry sessionEntry) (tea.Cmd, bool) {
	if m.application == nil {
		m.sessions.flash = "no application settings available"
		return nil, true
	}
	p := m.application.Preferences
	if p.Profile == nil {
		profile := m.config.Profile.Clone()
		p.Profile = &profile
	}
	if p.Workspace == "" {
		m.sessions.flash = "Choose an Agent workspace in Settings → Application first"
		return nil, true
	}
	stateFile := filepath.Join(entry.dir, "state.json")
	history, err := session.ReadHistory(stateFile)
	if err != nil {
		m.sessions.flash = fmt.Sprintf("Cannot open conversation: %v", err)
		return nil, true
	}
	m.saveDraft()
	draft := m.drafts[stateFile].text
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
