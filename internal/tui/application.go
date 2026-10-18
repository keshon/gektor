package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/session"
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
