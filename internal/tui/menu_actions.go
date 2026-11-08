package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/config"
)

// activateMenuAction is shared by keyboard and mouse menu activation.
func (m *model) activateMenuAction(command string) (tea.Model, tea.Cmd) {

	if strings.HasPrefix(command, "menu:") {
		m.header.menu = strings.TrimPrefix(command, "menu:")
		m.header.row = m.headerActionRow(0, 1)
		return m, nil
	}
	m.closeHeader()
	switch command {
	case "permissions-info":
		m.openPermissions()
		return m, nil
	case "help-keys", "help-commands":
		if command == "help-keys" && m.workflowActive() {
			return m.command("/workflows help")
		}
		m.openHelp()
		m.dialog.title = "Keyboard shortcuts"
		if command == "help-commands" {
			m.dialog.title = "Commands"
		}
		return m, nil
	case "appearance":
		m.openAppearance()
		return m, nil
	case "compact-session":
		m.confirmCompact()
		return m, nil
	case "screen:agent":
		if m.workflowActive() {
			return m.command("/workflows chat")
		}
		return m, nil
	case "screen:workflows":
		if !m.workflowActive() {
			return m.command("/workflows")
		}
		return m, nil
	case "about":
		return m.command("/about")
	case "workspace":
		m.openWorkspace()
		return m, nil
	case "prompt-info":
		m.inspectAgentInstructions()
		return m, nil
	case "rename-current":
		m.renameActive()
		return m, nil
	case "delete-current":
		m.deleteActive()
		return m, nil
	case "quit":
		return m.requestQuit()
	case "toggle-steering":
		value := "on"
		if m.steering {
			value = "off"
		}
		return m.command("/steering " + value)
	case "toggle-details":
		return m.handleKey(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	case "toggle-sys":
		return m.command("sys")
	case "toggle-labels":
		if err := m.changeAppearance(func(a *config.Appearance) { a.HideLabels = !m.hideLabels }); err != nil {
			m.openDialog("Appearance", []string{err.Error()})
		}
		return m, nil
	case "toggle-names", "toggle-tools":
		err := m.changeAppearance(func(a *config.Appearance) {
			switch command {
			case "toggle-names":
				a.HideNames = !m.hideNames
			case "toggle-tools":
				a.HideTools = !m.hideTools
			}
		})
		if err != nil {
			m.openDialog("Appearance", []string{err.Error()})
		}
		return m, nil
	case "latest":
		return m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	}
	if strings.HasPrefix(command, "edit:") {
		m.editSettings(strings.TrimPrefix(command, "edit:"))
		return m, nil
	}
	if command == "stop" {
		m.interrupted = true
		m.cancel()
		m.state = stStopping
		m.fitBottom()
		return m, nil
	}
	return m.command(command)
}
