package agent

import (
	"github.com/keshon/gektor/internal/tui/screen"
)

// menuDefinitions lists Agent-local command groups.
func (m *Model) menuDefinitions() []screen.Menu {
	return []screen.Menu{{ID: "session", Label: "Session"}, {ID: "execution", Label: "Run"}, {ID: "view", Label: "View"}}
}

func (m *Model) menuItems(menu string) []screen.MenuItem {
	idle := m.state == stDone
	sep := screen.MenuItem{}
	switch menu {
	case "overflow":
		visible := map[string]bool{}
		for _, item := range m.chrome.VisibleMenus() {
			visible[item.ID] = true
		}
		var rows []screen.MenuItem
		for _, item := range m.menuDefinitions() {
			if !visible[item.ID] {
				rows = append(rows, screen.MenuItem{Text: item.Label, Command: "menu:" + item.ID, Enabled: true})
			}
		}
		return rows
	case "session":
		saved := idle && m.stateFile != ""
		return append([]screen.MenuItem{{Text: "New session", Command: "/new", Enabled: idle}, {Text: "Open session…", Command: "/sessions", Enabled: true},
			{Text: "Rename session…", Command: "rename-current", Enabled: saved}, {Text: "Delete session…", Command: "delete-current", Enabled: saved}, sep,
			{Text: "Compact history…", Command: "compact-session", Enabled: saved}, sep}, m.sessionCopyItems()...)
	case "execution":
		return []screen.MenuItem{
			{Text: "Plan — read-only", Command: "/mode plan", Enabled: idle},
			{Text: "Act — allow changes", Command: "/mode act", Enabled: idle}, sep,
			{Text: "Ask permission", Command: "access-ask", Enabled: idle},
			{Text: "Full access", Command: "access-full", Enabled: idle}, sep,
			{Text: "Live steering", Command: "toggle-steering", Enabled: true},
			{Text: "Message queue…", Command: "message-queue", Enabled: true},
			{Text: "Background processes…", Command: "/processes", Enabled: true}, sep,
			{Text: "Permissions…", Command: "permissions-info", Enabled: true},
			{Text: "MCP servers…", Command: "/mcp", Enabled: true}, sep,
			{Text: "Stop run", Command: "stop", Enabled: m.state == stRunning},
			{Text: "Retry failed turn", Command: "/retry", Enabled: idle && m.runErr != nil && m.retryRun != nil},
		}
	case "view":
		return []screen.MenuItem{{Text: "Thinking", Command: "toggle-details", Enabled: true}, {Text: "Tool calls", Command: "toggle-tools", Enabled: true}, {Text: "System messages", Command: "toggle-sys", Enabled: true},
			{Text: "Speaker names", Command: "toggle-names", Enabled: true}, {Text: "Message labels", Command: "toggle-labels", Enabled: true}, sep, {Text: "Jump to latest", Command: "latest", Enabled: true}}
	}
	return nil
}

// menuToggle reports whether a menu row is a checkable toggle, and
// whether the state its label names is currently on.
//
// The checkmark sits in the label indent every menu row already carries,
// so it replaces an idle cell rather than adding one. A separate column
// cost two cells of the popup's width on every row to hold a mark that
// is blank on most of them.
//
// on is the state the label names, not the state a toggle flips to: the
// mark on "System messages" means scaffolding history is visible.
func (m *Model) menuToggle(command string) (toggle, on bool) {
	switch command {
	case "access-ask":
		return true, !m.approvals.FullAccess()
	case "access-full":
		return true, m.approvals.FullAccess()
	case "toggle-steering":
		return true, m.steering
	case "toggle-details":
		return true, !m.transcript.compact
	case "toggle-sys":
		return true, !m.hideSYS
	case "toggle-labels":
		return true, !m.hideLabels
	case "toggle-names":
		return true, !m.hideNames
	case "toggle-tools":
		return true, !m.hideTools
	case "screen:agent":
		return true, true
	case "screen:workflows":
		return true, false
	case "/mode plan":
		return true, m.plan
	case "/mode act":
		return true, !m.plan
	}
	return false, false
}

// menuShortcut names the key that reaches a menu row without opening
// the menu. A row with no shortcut still works by click.
func menuShortcut(command string) string {
	switch command {
	case "screen:agent":
		return ""
	case "/new":
		return "Ctrl+N"
	case "quit":
		return "Ctrl+Q"
	case "toggle-details":
		return "Ctrl+G"
	case "latest":
		return "Ctrl+End"
	case "message-queue":
		return "Alt+Q"
	case "stop":
		return "Esc"
	case "/help", "help-keys":
		return "F1"
	}
	return ""
}
