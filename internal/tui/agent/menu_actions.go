package agent

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/screen"
)

// activateMenuAction is shared by keyboard and mouse menu activation.
func (m *Model) activateMenuAction(command string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(command, "menu:") {
		m.chrome.Header.Menu = strings.TrimPrefix(command, "menu:")
		m.chrome.Header.Row = m.chrome.HeaderActionRow(0, 1)
		return m, nil
	}
	m.chrome.CloseHeader()
	switch command {
	case "copy-session-id", "copy-session-path":
		return m, m.copySession(command)
	case "message-queue":
		m.openInputQueue()
		return m, nil
	case "permissions-info":
		m.openPermissions()
		return m, nil
	case "help-keys", "help-commands":

		m.openHelp()
		m.chrome.Dialog.Title = "Keyboard shortcuts"
		if command == "help-commands" {
			m.chrome.Dialog.Title = "Commands"
		}
		return m, nil
	case "appearance":
		m.request(screen.ApplicationCommand{Command: "appearance"})
		return m, nil
	case "compact-session":
		m.confirmCompact()
		return m, nil
	case "screen:agent":
		return m, m.request(screen.Navigate{Screen: "agent"})
	case "screen:workflows":
		return m, m.request(screen.Navigate{Screen: "workflow"})
	case "about":
		return m.command("/about")
	case "workspace":
		m.request(screen.ApplicationCommand{Command: "workspace"})
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
			m.chrome.Open("Appearance", []string{err.Error()})
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
			m.chrome.Open("Appearance", []string{err.Error()})
		}
		return m, nil
	case "latest":
		return m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	}
	if strings.HasPrefix(command, "edit:") {
		m.request(screen.ApplicationCommand{Command: command})
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
