package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
)

func (m *Model) bindControls() {
	m.chrome.Opening = func(first bool) {
		if first {
			m.overlayScreen = m.active
		}
		if m.overlayScreen == agentScreen {
			m.agent.OverlayOpened(first)
		}
	}
	m.chrome.Closed = func() {
		if m.overlayScreen == agentScreen {
			m.agent.OverlayClosed()
		}
	}
	m.chrome.Changed = func() {
		if m.active == agentScreen {
			m.agent.ControlsChanged()
		}
		m.syncPresentation()
	}
}

func (m *Model) settingsCommands() []tea.Cmd {
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil {
		return nil
	}
	if cmd := m.chrome.Dialog.Form.DetailCommand(); cmd != nil {
		return []tea.Cmd{cmd}
	}
	return nil
}

func (m *Model) acceptSettings(msg tea.Msg) {
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil {
		return
	}
	switch msg := msg.(type) {
	case settings.DetailMsg:
		m.chrome.Dialog.Form.AcceptDetail(msg)
	case settings.ScanMsg:
		m.chrome.Dialog.Form.AcceptScan(msg)
	}
}

func (m *Model) applicationMenus(idle bool) []screen.Menu {
	editable := idle && m.profile != nil
	section := func(id string) screen.MenuItem {
		return screen.MenuItem{Text: settings.SectionTitle(id) + "…", Command: "edit:" + id, Enabled: editable}
	}
	menu := screen.Menu{ID: "settings", Label: "Settings", Items: []screen.MenuItem{section("model"), section("generation"), section("profiles")}}
	if m.profile != nil {
		menu.Items = append(menu.Items, screen.MenuItem{}, section("you"), section("prompts"))
	}
	menu.Items = append(menu.Items, screen.MenuItem{}, screen.MenuItem{Text: "Permissions…", Command: "permissions-info", Enabled: true}, screen.MenuItem{Text: "MCP servers…", Command: "/mcp", Enabled: true}, screen.MenuItem{}, screen.MenuItem{Text: "Appearance…", Command: "appearance", Enabled: true})
	help := screen.Menu{ID: "help", Label: "Help", Items: []screen.MenuItem{{Text: "Keyboard shortcuts", Command: "help-keys", Enabled: true}}}
	if !m.workflowActive() {
		help.Items = append(help.Items, screen.MenuItem{Text: "Commands", Command: "help-commands", Enabled: true})
		if m.profile != nil {
			help.Items = append(help.Items, screen.MenuItem{Text: "Inspect agent instructions…", Command: "prompt-info", Enabled: true})
		}
	}
	help.Items = append(help.Items, screen.MenuItem{}, screen.MenuItem{Text: "About GEKTOR", Command: "about", Enabled: true})
	if m.workflowActive() {
		return []screen.Menu{help}
	}
	return []screen.Menu{menu, help}
}
