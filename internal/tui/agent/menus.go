package agent

import (
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
)

type menuItem = screen.MenuItem

type menuDefinition = screen.Menu

// menuDefinitions describes application navigation independently of header layout.
func (m *Model) menuDefinitions() []menuDefinition {
	menus := []menuDefinition{{ID: "app", Label: "GEKTOR"}}
	menus = append(menus, menuDefinition{ID: "session", Label: "Session"})
	return append(menus, menuDefinition{ID: "execution", Label: "Run"}, menuDefinition{ID: "view", Label: "View"}, menuDefinition{ID: "settings", Label: "Settings"}, menuDefinition{ID: "help", Label: "Help"})
}

// A narrow terminal keeps every menu reachable through More rather than clipping it.
func (m *Model) visibleMenus() []menuDefinition {
	m.syncMenus()
	return m.chrome.VisibleMenus()
}

func (m *Model) menuItems(menu string) []menuItem {
	idle := m.state == stDone
	sep := menuItem{}
	switch menu {
	case "overflow":
		visible := map[string]bool{}
		for _, item := range m.visibleMenus() {
			visible[item.ID] = true
		}
		var rows []menuItem
		for _, item := range m.menuDefinitions() {
			if !visible[item.ID] {
				rows = append(rows, menuItem{Text: item.Label, Command: "menu:" + item.ID, Enabled: true})
			}
		}
		return rows
	case "app":
		return []menuItem{{Text: "Agent", Command: "screen:agent", Enabled: idle}, {Text: "Workflows", Command: "screen:workflows", Enabled: idle}, sep,
			{Text: "Change workspace…", Command: "workspace", Enabled: idle && m.application != nil}, sep, {Text: "Quit", Command: "quit", Enabled: true}}
	case "session":
		saved := idle && m.stateFile != ""
		return append([]menuItem{{Text: "New session", Command: "/new", Enabled: idle}, {Text: "Open session…", Command: "/sessions", Enabled: true},
			{Text: "Rename session…", Command: "rename-current", Enabled: saved}, {Text: "Delete session…", Command: "delete-current", Enabled: saved}, sep,
			{Text: "Compact history…", Command: "compact-session", Enabled: saved}, sep}, m.sessionCopyItems()...)
	case "execution":
		return []menuItem{{Text: "Plan — read-only", Command: "/mode plan", Enabled: idle}, {Text: "Act — allow changes", Command: "/mode act", Enabled: idle}, sep,
			{Text: "Stop run", Command: "stop", Enabled: m.state == stRunning}, {Text: "Retry failed turn", Command: "/retry", Enabled: idle && m.runErr != nil && m.retryRun != nil}, sep,
			{Text: "Message queue…", Command: "message-queue", Enabled: true}, {Text: "Background processes…", Command: "/processes", Enabled: true}, {Text: "Apply new messages during run", Command: "toggle-steering", Enabled: true}}
	case "view":
		return []menuItem{{Text: "Thinking", Command: "toggle-details", Enabled: true}, {Text: "Tool calls", Command: "toggle-tools", Enabled: true}, {Text: "System messages", Command: "toggle-sys", Enabled: true},
			{Text: "Speaker names", Command: "toggle-names", Enabled: true}, {Text: "Message labels", Command: "toggle-labels", Enabled: true}, sep, {Text: "Jump to latest", Command: "latest", Enabled: true}}
	case "settings":
		editable := idle && m.config != nil
		section := func(id string) menuItem {
			return menuItem{Text: settings.SectionTitle(id) + "…", Command: "edit:" + id, Enabled: editable}
		}
		rows := []menuItem{section("model"), section("generation"), section("profiles")}
		if m.config != nil {
			rows = append(rows, sep, section("you"), section("prompts"))
		}
		return append(rows, sep, menuItem{Text: "Permissions…", Command: "permissions-info", Enabled: true}, menuItem{Text: "MCP servers…", Command: "/mcp", Enabled: true}, sep, menuItem{Text: "Appearance…", Command: "appearance", Enabled: true})
	case "help":
		rows := []menuItem{{Text: "Keyboard shortcuts", Command: "help-keys", Enabled: true}, {Text: "Commands", Command: "help-commands", Enabled: true}}
		if m.config != nil {
			rows = append(rows, menuItem{Text: "Inspect agent instructions…", Command: "prompt-info", Enabled: true})
		}
		return append(rows, sep, menuItem{Text: "About GEKTOR", Command: "about", Enabled: true})
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
