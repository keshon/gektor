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
func (m *model) settingsApplicationChanged() bool {
	f := m.dialog.form
	fields, ok := f.Workspace.Draft.Fields("application")
	if !ok {
		return false
	}
	p := m.application.Preferences
	for _, field := range fields {
		switch field.Key {
		case "app-workspace":
			if field.Value != p.Workspace {
				return true
			}
		}
	}
	return false
}

func (m *model) settingsWorkspace() *settingsWorkspace {
	if m.dialog == nil || m.dialog.form == nil {
		return nil
	}
	return m.dialog.form.Workspace
}
func (m *model) saveApplication() error {
	if m.application == nil || m.application.Temporary {
		return nil
	}
	p := m.application.Preferences
	if m.config != nil {
		profile := m.config.Profile.Clone()
		p.Profile = &profile
	}
	if err := m.saveSessionSettings(); err != nil {
		return err
	}
	return config.SavePreferences(m.application.PreferencePath, p)
}

// DialogueProjection carries text context without exposing historical tool calls.
func DialogueProjection(history []llm.Message) []llm.Message {
	out := []llm.Message{}
	var user, answer string
	flush := func() {
		if user != "" && answer != "" {
			out = append(out, llm.Message{Role: llm.RoleUser, Content: user}, llm.Message{Role: llm.RoleAssistant, Content: answer})
		}
		user, answer = "", ""
	}
	for _, m := range history {
		switch m.Role {
		case llm.RoleUser:
			flush()
			user = m.Content
		case llm.RoleAssistant:
			if m.Content != "" {
				if answer != "" {
					answer += "\n"
				}
				answer += m.Content
			}
		case llm.RoleTool:
			if user != "" {
				answer += "\n[Prior tool output] " + m.Content
			}
		}
	}
	flush()
	return out
}

// requestProfileChange replaces owned services without starting an Agent run.
func (m *model) requestProfileChange(profile config.Profile) error {
	if m.application == nil {
		return fmt.Errorf("application settings are unavailable for this launch")
	}
	if err := profile.Validate(); err != nil {
		return err
	}

	if profile.Provider.Ownership == "managed" {
		for _, path := range []string{profile.Provider.Runtime, profile.Provider.ModelFile} {
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("model configuration: %w", err)
			}
			if info.IsDir() {
				return fmt.Errorf("select a file: %s", path)
			}
		}
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
		// screen with no way to create one, and the bundled Agent profile
		// carries no conversation block at all, so this was the ordinary
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
