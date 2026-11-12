package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/capabilities/ssh"
	toolcatalog "github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/tools/extension"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/screen"
	toolsui "github.com/keshon/gektor/internal/tui/tools"
)

type toolPage uint8

const (
	toolList toolPage = iota
	toolMedia
	toolMediaEdit
	toolDetails
	toolMediaDetails
	toolSSH
	toolSSHEdit
	toolSSHDetails
	toolSSHSecret
)

func (m *Model) toolsIdle() bool {
	return m.agent.Status().Idle && (m.workflows == nil || !m.workflows.State().Busy)
}

func (m *Model) openTools() {
	if !m.toolsIdle() {
		return
	}
	sources := append([]*extension.Capability(nil), m.launch.Env.Capabilities...)
	if m.launch.Env.CapabilitySources != nil {
		sources = append(sources, m.launch.Env.CapabilitySources()...)
	}
	sources = toolcatalog.WithBuiltinCapabilities(sources)
	if m.launch.Env.Browser != nil {
		sources = append(sources, m.launch.Env.Browser.Capability())
	}
	sets, err := app.Toolsets(toolcatalog.Packages(), sources, m.launch.Env.NativeExposure, m.launch.Env.MCPTools)
	if err != nil {
		m.chrome.PresentDialog(screen.Dialog{Title: "Tools", Lines: []string{err.Error()}})
		return
	}
	m.toolsets = sets
	view := toolsui.Inventory(sets)
	for i, row := range view.Table.Model.Rows {
		if row.ID == m.toolSelection {
			view.Table.Model.Cursor = i
			break
		}
	}
	m.chrome.PresentDialog(view)
	m.toolsDialog = m.chrome.Dialog
	m.toolsPage = toolList
}

func (m *Model) openMediaTools() {
	view := m.tools.Media(m.toolsEditable())
	for i, row := range view.Table.Model.Rows {
		if row.ID == m.mediaSelection {
			view.Table.Model.Cursor = i
			break
		}
	}
	m.chrome.PresentDialog(view)
	m.toolsDialog = m.chrome.Dialog
	m.toolsPage = toolMedia
}

func (m *Model) toolsEditable() bool {
	return m.toolsIdle() && m.profile != nil && m.application != nil && (m.workflows == nil || !m.workflows.Dirty())
}

func (m *Model) toolAction(event screen.Event) tea.Cmd {
	if m.toolsPage == toolSSH {
		return m.sshToolAction(event)
	}
	if m.toolsPage == toolMedia {
		m.mediaSelection = event.Row
		if event.Action == "check" {
			cmd := m.tools.Check(m.base, event.Row)
			m.refreshMediaTools()
			return cmd
		}
		if event.Action == "operations" {
			for _, set := range m.toolsets {
				if set.ID == "native_media" {
					m.chrome.PresentDialog(toolsui.Details(set))
					m.toolsDialog = m.chrome.Dialog
					m.toolsPage = toolMediaDetails
					break
				}
			}
			return nil
		}
		if event.Action == "details" && m.toolsEditable() {
			preferences, err := m.tools.Preferences(event.Row)
			if err != nil {
				m.chrome.PresentDialog(screen.Dialog{Title: "Media preferences", Lines: []string{err.Error()}})
				m.toolsDialog = m.chrome.Dialog
				m.toolsPage = toolDetails
				return nil
			}
			m.openForm(preferences, func(values map[string]string) error { return m.applyExecutablePreference(event.Row, values) })
			m.toolsDialog = m.chrome.Dialog
			m.toolsPage = toolMediaEdit
		}
		return nil
	}
	m.toolSelection = event.Row
	for _, set := range m.toolsets {
		if set.ID != event.Row {
			continue
		}
		if set.ID == "native_ssh" {
			m.openSSHTools()
			return nil
		}
		if set.ID == "native_media" {
			m.openMediaTools()
			return nil
		}
		m.chrome.PresentDialog(toolsui.Details(set))
		m.toolsDialog = m.chrome.Dialog
		m.toolsPage = toolDetails
		return nil
	}
	return nil
}

func (m *Model) refreshMediaTools() {
	if m.toolsDialog == nil || m.chrome.Dialog != m.toolsDialog || m.toolsPage != toolMedia {
		return
	}
	view := m.tools.Media(m.toolsEditable())
	m.chrome.RefreshTable(view.Table.Model.Rows)
	m.chrome.Dialog.Table.Description = view.Table.Description
	m.chrome.Dialog.Table.Actions = view.Table.Actions
}

func (m *Model) toolPreferencesRestart() (*ModeChange, error) {
	if !m.toolsEditable() {
		return nil, fmt.Errorf("finish active work and save or discard the retained workflow before applying tool preferences")
	}
	snapshot, err := m.agent.Snapshot()
	if err != nil {
		return nil, err
	}
	preferences := m.application.Preferences
	profile := m.profile.Profile.Clone()
	preferences.Profile = &profile
	err = preferences.Validate()
	if err != nil {
		return nil, err
	}
	snapshot.Preferences = preferences
	return snapshot, nil
}

func (m *Model) applyExecutablePreference(name string, values map[string]string) error {
	snapshot, err := m.toolPreferencesRestart()
	if err != nil {
		return err
	}
	err = m.tools.Save(name, values)
	if err != nil {
		return err
	}
	m.modeChange = snapshot
	m.commands = append(m.commands, tea.Quit)
	return nil
}

func (m *Model) openSSHTools() {
	view := m.tools.SSH(m.toolsEditable())
	for i, row := range view.Table.Model.Rows {
		if row.ID == m.sshSelection {
			view.Table.Model.Cursor = i
			break
		}
	}
	m.chrome.PresentDialog(view)
	m.toolsDialog, m.toolsPage = m.chrome.Dialog, toolSSH
}

func (m *Model) sshToolAction(event screen.Event) tea.Cmd {
	if event.Action == "operations" {
		for _, set := range m.toolsets {
			if set.ID == "native_ssh" {
				m.chrome.PresentDialog(toolsui.Details(set))
				m.toolsDialog, m.toolsPage = m.chrome.Dialog, toolSSHDetails
				break
			}
		}
		return nil
	}
	m.sshSelection = event.Row
	if event.Action == "test" {
		cmd := m.tools.CheckSSH(m.base, event.Row)
		view := m.tools.SSH(m.toolsEditable())
		m.chrome.Dialog.Table.Description = view.Table.Description
		m.chrome.Dialog.Table.Actions = view.Table.Actions
		return cmd
	}
	if !m.toolsEditable() {
		return nil
	}
	name := event.Row
	if event.Action == "add" {
		name = ""
	}
	if event.Action == "add" || event.Action == "edit" {
		preferences, err := m.tools.SSHPreferences(name)
		if err != nil {
			m.chrome.PresentDialog(screen.Dialog{Title: "SSH preferences", Lines: []string{err.Error()}})
			m.toolsDialog, m.toolsPage = m.chrome.Dialog, toolSSHDetails
			return nil
		}
		m.openForm(preferences, func(values map[string]string) error { return m.applySSHPreference(name, values) })
		m.toolsDialog, m.toolsPage = m.chrome.Dialog, toolSSHEdit
	}
	if event.Action == "delete" {
		confirmation := screen.Form{
			Title:         "Delete SSH connection",
			Hint:          "Type the connection name to confirm. Saved workflows using it will need another connection. Apply restarts GEKTOR; restart independent workers separately.",
			ApplyLabel:    "Delete and restart",
			Destructive:   true,
			StartOnCancel: true,
			Fields:        []form.Field{{Key: "confirmation", Label: "Connection: " + name}},
		}
		m.openForm(confirmation, func(values map[string]string) error { return m.deleteSSHPreference(name, values["confirmation"]) })
		m.toolsDialog, m.toolsPage = m.chrome.Dialog, toolSSHEdit
	}
	return nil
}

func (m *Model) applySSHPreference(name string, values map[string]string) error {
	snapshot, err := m.toolPreferencesRestart()
	if err != nil {
		return err
	}
	settings, err := m.tools.SSHSettings(name, values)
	if err != nil {
		return err
	}
	if settings.NewSecret != "" {
		m.sshSecret = &settings
		title := "SSH password: " + settings.Name
		if settings.NewSecret == ssh.PassphraseSecret {
			title = "SSH key passphrase: " + settings.Name
		}
		m.chrome.PresentSecret(title, "Encrypted for your Windows account. Save confirms this connection and restarts GEKTOR. Cancel leaves it unchanged.")
		m.toolsDialog, m.toolsPage = m.chrome.Dialog, toolSSHSecret
		return nil
	}
	err = m.tools.SaveSSH(settings)
	if err != nil {
		return err
	}
	m.modeChange = snapshot
	m.commands = append(m.commands, tea.Quit)
	return nil
}

func (m *Model) deleteSSHPreference(name, confirmation string) error {
	snapshot, err := m.toolPreferencesRestart()
	if err != nil {
		return err
	}

	err = m.tools.DeleteSSH(name, confirmation)
	if err != nil {
		return err
	}

	m.modeChange = snapshot
	m.commands = append(m.commands, tea.Quit)
	return nil
}

func (m *Model) applySSHSecret(dialog *screen.DialogState) {
	value := dialog.Secret.Take()
	snapshot, err := m.toolPreferencesRestart()
	if err == nil {
		settings := m.sshSecret
		err = m.tools.SSHProfiles.SaveWithSecret(settings.Name, settings.Profile, settings.NewSecret, value)
	}
	value = ""
	if err != nil {
		dialog.Secret.Error = err.Error()
		return
	}
	m.sshSecret = nil
	m.modeChange = snapshot
	m.commands = append(m.commands, tea.Quit)
	m.chrome.Close()
}
