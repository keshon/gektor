package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/app"
	toolcatalog "github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/tools/extension"
	"github.com/keshon/gektor/internal/tui/screen"
	toolsui "github.com/keshon/gektor/internal/tui/tools"
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
	m.chrome.PresentDialog(toolsui.Inventory(sets))
	m.toolsDialog = m.chrome.Dialog
	m.toolsPage = "list"
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
	m.toolsPage = "media"
}

func (m *Model) toolsEditable() bool {
	return m.toolsIdle() && m.profile != nil && m.application != nil && (m.workflows == nil || !m.workflows.Dirty())
}

func (m *Model) toolAction(event screen.Event) tea.Cmd {
	if m.toolsPage == "media" {
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
					m.toolsPage = "media-detail"
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
				m.toolsPage = "detail"
				return nil
			}
			m.openForm(preferences, func(values map[string]string) error { return m.applyExecutablePreference(event.Row, values) })
			m.toolsDialog = m.chrome.Dialog
			m.toolsPage = "edit"
		}
		return nil
	}
	for _, set := range m.toolsets {
		if set.ID != event.Row {
			continue
		}
		if set.ID == "native_media" {
			m.openMediaTools()
			return nil
		}
		m.chrome.PresentDialog(toolsui.Details(set))
		m.toolsDialog = m.chrome.Dialog
		m.toolsPage = "detail"
		return nil
	}
	return nil
}

func (m *Model) refreshMediaTools() {
	if m.toolsDialog == nil || m.chrome.Dialog != m.toolsDialog || m.toolsPage != "media" {
		return
	}
	view := m.tools.Media(m.toolsEditable())
	m.chrome.RefreshTable(view.Table.Model.Rows)
	m.chrome.Dialog.Table.Description = view.Table.Description
	m.chrome.Dialog.Table.Actions = view.Table.Actions
}

func (m *Model) applyExecutablePreference(name string, values map[string]string) error {
	if !m.toolsEditable() {
		return fmt.Errorf("finish active work and save or discard the retained workflow before applying tool preferences")
	}
	snapshot, err := m.agent.Snapshot()
	if err != nil {
		return err
	}
	preferences := m.application.Preferences
	profile := m.profile.Profile.Clone()
	preferences.Profile = &profile
	err = preferences.Validate()
	if err != nil {
		return err
	}
	err = m.tools.Save(name, values)
	if err != nil {
		return err
	}
	snapshot.Preferences = preferences
	m.modeChange = snapshot
	m.commands = append(m.commands, tea.Quit)
	return nil
}
