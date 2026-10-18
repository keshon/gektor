package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/agent"
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

// saveHarness persists the loop's own settings: step budget, nudge level and
// reasoning budget.
//
// These used to be flags and live model fields only. A step budget set with
// /max-steps was therefore still on screen when the process closed and gone
// by the next launch, and the only way to keep it was to pass it on the
// command line every time - which is exactly what a preferences file is for.
//
// MaxSteps is stored as the budget actually in force, so "no limit" survives
// as itself rather than as an unset zero that would read as the default.
func (m *model) saveHarness() error {
	if m.application == nil || m.application.Temporary {
		return nil
	}
	steps := 0
	switch {
	case m.maxStepsOverride == noStepLimit:
		steps = unlimitedSteps
	case m.maxStepsOverride > 0:
		steps = m.maxStepsOverride
	}
	m.application.Preferences.Harness = config.Harness{
		MaxSteps:    steps,
		Nudges:      agent.NudgeLevelName(m.nudges),
		ThinkBudget: m.thinkBudget,
	}
	return m.savePreferences()
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
