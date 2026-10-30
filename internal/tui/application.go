package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
)

type Application struct {
	Preferences    config.Preferences
	PreferencePath string
	Temporary      bool
}
type ModeChange struct {
	Preferences config.Preferences
	History     []llm.Message
	Draft       string
	Directory   string
	KeepAct     bool
}

func (m *ModeChange) Error() string { return "application settings changed" }
func (m *model) openApplication() {
	if m.application != nil && m.state == stDone {
		m.editSettings("application")
	}
}

func (m *model) saveApplication() error { return m.savePreferences() }

// requestProfileChange replaces owned services without starting an Agent run.
func (m *model) requestProfileChange(profile config.Profile) error {
	if m.application == nil {
		return fmt.Errorf("application settings are unavailable for this launch")
	}
	if err := profile.Validate(); err != nil {
		return err
	}

	p := m.application.Preferences
	p.Profile = &profile
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.Workspace != nil && m.dialog.form.Workspace.LoadedPath != "" {
		p.ProfilePath = m.dialog.form.Workspace.LoadedPath
	}
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.Workspace != nil {
		fields, _ := m.dialog.form.Workspace.Draft.Fields("application")
		for _, field := range fields {
			if field.Key == "app-workspace" {
				p.Workspace = strings.TrimSpace(field.Value)
			}
		}
	}
	if err := p.Validate(); err != nil {
		return err
	}
	{
		absolute, err := filepath.Abs(p.Workspace)
		if err != nil {
			return err
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("select an existing Agent workspace")
		}
		p.Workspace = absolute
	}
	if profile.Identity == nil {
		// Materialize rather than refuse. Refusing left the reader on a
		// screen with no way to create one, and the bundled profile
		// carries no identity block at all, so this was the ordinary
		// path rather than an edge case.
		profile.Identity = &config.Identity{
			UserName:     config.DefaultUserName,
			SystemPrompt: config.DefaultSystemPrompt,
		}
	}
	p.Profile = &profile
	// Read the history before writing anything. A read failure used to leave
	// a profiles/custom file on disk holding settings that were never
	// applied and that nothing referenced, and left the workspace pointing
	// at it as though the apply had landed.
	history := append([]llm.Message(nil), m.seedHistory...)
	if m.stateFile != "" {
		loaded, err := session.ReadHistory(m.stateFile)
		if err != nil {
			return err
		}
		history = loaded
	}
	if err := m.saveCustomSettings(profile); err != nil {
		return err
	}
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.Workspace != nil && m.dialog.form.Workspace.LoadedPath != "" {
		p.ProfilePath = m.dialog.form.Workspace.LoadedPath
	}
	m.modeChange = &ModeChange{Preferences: p, History: history, Draft: m.input.Value(), Directory: m.sessionDirectory(), KeepAct: !m.plan}
	m.pendingCommands = append(m.pendingCommands, tea.Quit)
	return nil
}

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
	p.Mode = strings.ToLower(m.modeName())
	p.Appearance = m.currentAppearance()
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
	m.application.Preferences = p
	return nil
}

// saveHarness persists the step limit, including the explicit unlimited value.
func (m *model) saveHarness() (saveErr error) {
	if m.application == nil {
		return nil
	}
	previous := m.application.Preferences.Harness
	defer func() {
		if saveErr != nil {
			m.application.Preferences.Harness = previous
		}
	}()
	steps := 0
	switch {
	case m.maxStepsOverride == noStepLimit:
		steps = unlimitedSteps
	case m.maxStepsOverride > 0:
		steps = m.maxStepsOverride
	}
	m.application.Preferences.Harness = config.Harness{
		MaxSteps: steps,
	}
	if m.application.Temporary {
		p, err := config.LoadPreferences(m.application.PreferencePath)
		if err != nil {
			return err
		}
		p.Harness = m.application.Preferences.Harness
		return config.SavePreferences(m.application.PreferencePath, p)
	}
	return m.savePreferences()
}

// An explicit workflow choice is global even when this launch uses a temporary
// backend or workspace. Save only the choice, preserving the installed profile.
func (m *model) saveDefaultMode() error {
	if m.application == nil || !m.application.Temporary {
		return m.savePreferences()
	}
	p, err := config.LoadPreferences(m.application.PreferencePath)
	if err != nil {
		return err
	}
	p.Mode = strings.ToLower(m.modeName())
	if err := config.SavePreferences(m.application.PreferencePath, p); err != nil {
		return err
	}
	m.application.Preferences.Mode = p.Mode
	return nil
}

// Agent installs its current system instruction when resuming. Never carry the
// old session's leading prompt as a second, competing system instruction.
func agentSeed(history []llm.Message) []llm.Message {
	for len(history) > 0 && history[0].Role == llm.RoleSystem {
		history = history[1:]
	}
	return append([]llm.Message{{Role: llm.RoleSystem}}, history...)
}
