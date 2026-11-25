package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func (m *Model) openWorkspace() {
	if m.application == nil || m.profile == nil || !m.agent.Status().Idle {
		return
	}
	if m.workflows != nil && m.workflows.State().Busy {
		return
	}
	fields := []form.Field{{
		Key: "app-workspace", Label: "Workspace", Value: m.application.Preferences.Workspace,
	}}
	m.openForm(screen.Form{
		Title:      "Workspace",
		Hint:       "Apply restarts GEKTOR in the selected workspace.",
		ApplyLabel: "Apply",
		Fields:     fields,
		FileFields: map[string]fieldform.FilePicker{"app-workspace": {Root: m.application.Preferences.Workspace, Directory: true}},
	}, func(_ map[string]string) error {
		return m.requestProfileChange(m.profile.Profile.Clone())
	})
}

// requestProfileChange replaces owned services without starting an Agent run.
func (m *Model) requestProfileChange(profile config.Profile) error {
	if m.workflows != nil && m.workflows.Dirty() {
		return fmt.Errorf("save or discard the retained workflow before changing application settings")
	}
	if m.application == nil {
		return fmt.Errorf("application settings are unavailable for this launch")
	}
	if err := profile.Validate(); err != nil {
		return err
	}

	snapshotForm, formOpen := m.chrome.FormSnapshot(m.chrome.DialogID())
	p := m.application.Preferences
	p.Profile = &profile
	if formOpen && (m.preferences == nil || m.preferences.EditSession == nil) {
		for _, field := range snapshotForm.Fields {
			if field.Key == "app-workspace" {
				p.Workspace = strings.TrimSpace(field.Value)
			}
		}
	}
	if formOpen && m.preferences != nil && m.preferences.EditSession != nil && m.preferences.EditSession.LoadedPath != "" {
		p.ProfilePath = m.preferences.EditSession.LoadedPath
	}
	if formOpen && m.preferences != nil && m.preferences.EditSession != nil {
		if fields, ok := m.preferences.EditSession.Draft.Fields("network"); ok {
			network := config.Network{}
			for _, field := range fields {
				switch field.Key {
				case "network-type":
					network.Type = field.Value
				case "network-host":
					network.Host = strings.Trim(strings.TrimSpace(field.Value), "[]")
				case "network-port":
					network.Port = strings.TrimSpace(field.Value)
				}
			}
			if network.Type == "" {
				network = config.Network{}
			}
			p.Network = network
		}
		fields, _ := m.preferences.EditSession.Draft.Fields("workspace")
		for _, field := range fields {
			if field.Key == "app-workspace" {
				p.Workspace = strings.TrimSpace(field.Value)
			}
		}
	}
	if err := p.Validate(); err != nil {
		return err
	}
	absolute, err := filepath.Abs(p.Workspace)
	if err != nil {
		return err
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("select an existing workspace")
	}
	p.Workspace = absolute
	if profile.Identity == nil {
		// Bundled profiles may omit identity; the relaunch needs explicit defaults.
		profile.Identity = &config.Identity{
			UserName:     config.DefaultUserName,
			SystemPrompt: config.DefaultSystemPrompt,
		}
	}
	p.Profile = &profile
	// Read history before saving a profile so a failed relaunch leaves no new files.
	snapshot, err := m.agent.Snapshot()
	if err != nil {
		return err
	}
	if err := m.saveCustomSettings(profile); err != nil {
		return err
	}
	if formOpen && m.preferences != nil && m.preferences.EditSession != nil && m.preferences.EditSession.LoadedPath != "" {
		p.ProfilePath = m.preferences.EditSession.LoadedPath
	}
	workspaceRoot := ""
	if m.launch.Env.WS != nil {
		workspaceRoot = m.launch.Env.WS.Root()
	}
	snapshot.ReassignWorkspace = !sameWorkspace(p.Workspace, workspaceRoot)
	snapshot.Preferences = p
	m.modeChange = snapshot
	m.commands = append(m.commands, tea.Quit)
	return nil
}

func sameWorkspace(a, b string) bool {
	left, e := filepath.Abs(a)
	if e != nil {
		return false
	}
	right, e := filepath.Abs(b)
	return e == nil && strings.EqualFold(left, right)
}
