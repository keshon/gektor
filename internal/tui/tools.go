package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/conversation"
	toolcatalog "github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/tools/extension"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/table"
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
	return m.toolLoadCancel == nil && m.agent.Status().Idle && (m.workflows == nil || !m.workflows.State().Busy)
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
	sets, err := app.Toolsets(toolcatalog.Packages(), sources, m.launch.Env.NativeExposure, m.launch.Env.MCPTools)
	if err != nil {
		m.chrome.PresentDialog(screen.Dialog{Title: "Tools", Lines: []string{err.Error()}})
		return
	}
	states, attached, stateErr := m.agent.ToolsetState()
	if stateErr != nil {
		m.chrome.PresentDialog(screen.Dialog{Title: "Tools", Lines: []string{stateErr.Error()}})
		return
	}
	m.toolSessionAttached = attached && !m.workflowActive()
	for i := range sets {
		sets[i].Status = "No session"
		if !m.toolSessionAttached {
			continue
		}
		sets[i].Status = "Not loaded"
		if strings.HasPrefix(sets[i].Connection, "failed") {
			sets[i].Status = "Unavailable"
		}
		if sets[i].Loading == "Always" {
			sets[i].Status = "Loaded"
		}
		for _, state := range states {
			id := strings.TrimPrefix(sets[i].ID, "source:")
			if state.ID == id && !state.Unloaded {
				sets[i].Status = "Loaded"
				if len(sets[i].Agent) == 0 {
					for _, tool := range state.Tools {
						sets[i].Agent = append(sets[i].Agent, tool.Name)
					}
				}
			}
		}
	}
	m.toolsets = sets
	view := toolsui.Inventory(sets)
	if m.toolsTable != nil {
		m.toolsTable.SetRows(view.Table.Model.Rows)
		view.Table.Model = m.toolsTable
	} else {
		m.toolsTable = view.Table.Model
	}
	m.chrome.PresentDialog(view)
	m.toolsDialog = m.chrome.Dialog
	m.toolsPage = toolList
	m.syncToolActions()
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
	for _, set := range m.toolsets {
		if set.ID != event.Row {
			continue
		}
		if event.Action == "load" || event.Action == "unload" {
			if !m.toolsIdle() || !m.toolSessionAttached || set.Loading == "Always" {
				return nil
			}
			change, err := m.agent.ToolsetChange(strings.TrimPrefix(set.ID, "source:"), event.Action == "load")
			if err != nil {
				m.chrome.PresentDialog(screen.Dialog{Title: "Tools", Lines: []string{err.Error()}})
				return nil
			}
			ctx, cancel := context.WithTimeout(m.base, 4*time.Minute)
			m.toolLoadCancel = cancel
			m.toolLoadDone = make(chan struct{})
			done := m.toolLoadDone
			result := make(chan toolsetChanged, 1)
			go func() {
				defer close(done)
				states, err := change.Apply(ctx)
				result <- toolsetChanged{states: states, err: err}
			}()
			m.chrome.PresentDialog(screen.Dialog{Title: "Tools", Lines: []string{"Updating " + set.Name + "…", "Escape cancels."}})
			return func() tea.Msg { return <-result }
		}
		if event.Action == "details" {
			m.chrome.PresentDialog(toolsui.Details(set))
			m.toolsDialog = m.chrome.Dialog
			m.toolsPage = toolDetails
			return nil
		}
		if event.Action != "settings" {
			return nil
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

type toolsetChanged struct {
	states conversation.Snapshot
	err    error
}

func (m *Model) acceptToolsetChange(msg toolsetChanged) {
	<-m.toolLoadDone
	m.toolLoadCancel()
	m.toolLoadCancel, m.toolLoadDone = nil, nil
	if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
		m.chrome.PresentDialog(screen.Dialog{Title: "Tools", Lines: []string{msg.err.Error()}})
		m.toolsDialog = m.chrome.Dialog
		m.toolsPage = toolDetails
		return
	}
	if msg.err == nil {
		m.agent.AcceptToolsetState(msg.states)
	}
	m.openTools()
}

func (m *Model) syncToolActions() {
	if m.toolsDialog == nil || m.chrome.Dialog != m.toolsDialog || m.toolsPage != toolList || m.chrome.Dialog.Table == nil {
		return
	}
	selected := m.chrome.Dialog.Table.Model.Selected()
	m.chrome.Dialog.Table.Actions = []table.Action{
		{ID: "details", Label: "Details", Key: "enter", Enabled: false},
		{ID: "close", Label: "Close", Key: "esc", Enabled: true},
	}
	for _, set := range m.toolsets {
		if set.ID != selected {
			continue
		}
		action, label := "load", "Load"
		if set.Status == "Loaded" {
			action, label = "unload", "Unload"
		}
		settings := set.ID == "native_media" || set.ID == "native_ssh"
		m.chrome.Dialog.Table.Actions = []table.Action{
			{ID: action, Label: label, Key: "l", Enabled: m.toolsIdle() && m.toolSessionAttached && set.Loading != "Always"},
			{ID: "details", Label: "Details", Key: "enter", Enabled: true},
			{ID: "settings", Label: "Settings", Key: "s", Enabled: settings && m.toolsIdle()},
			{ID: "close", Label: "Close", Key: "esc", Enabled: true},
		}
		break
	}
}
