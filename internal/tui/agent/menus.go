package agent

import (
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/internal/tui/settings"
	workflowui "github.com/keshon/gektor/internal/tui/workflow"
)

type menuItem struct {
	text, command string
	enabled       bool
}

type menuDefinition struct{ id, label string }

// menuDefinitions describes application navigation independently of header layout.
func (m *Model) menuDefinitions() []menuDefinition {
	menus := []menuDefinition{{"app", "GEKTOR"}}
	if m.workflowActive() {
		for _, menu := range workflowui.Menus() {
			menus = append(menus, menuDefinition{id: menu.ID, label: menu.Label})
		}
	} else {
		menus = append(menus, menuDefinition{"session", "Session"})
	}
	return append(menus, menuDefinition{"execution", "Run"}, menuDefinition{"view", "View"}, menuDefinition{"settings", "Settings"}, menuDefinition{"help", "Help"})
}

// A narrow terminal keeps every menu reachable through More rather than clipping it.
func (m *Model) visibleMenus() []menuDefinition {
	menus := m.menuDefinitions()
	width := 1
	for _, menu := range menus {
		width += lipgloss.Width(menu.label) + 3
	}
	if width <= m.termW {
		return menus
	}
	used, count := 1, 0
	for _, menu := range menus {
		if used+lipgloss.Width(menu.label)+3+lipgloss.Width("More…")+3 > m.termW {
			break
		}
		used += lipgloss.Width(menu.label) + 3
		count++
	}
	return append(menus[:count:count], menuDefinition{"overflow", "More…"})
}

func (m *Model) menuItems(menu string) []menuItem {
	if m.workflows != nil && (m.workflowActive() || menu == "recipe" || menu == "node") {
		if items := m.workflows.MenuItems(menu, m.state == stDone); items != nil {
			rows := make([]menuItem, 0, len(items))
			for _, item := range items {
				rows = append(rows, menuItem{text: item.Text, command: item.Command, enabled: item.Enabled})
			}
			return rows
		}
	}

	idle := m.state == stDone && (m.workflows == nil || !m.workflows.State().Busy)
	sep := menuItem{}
	switch menu {
	case "overflow":
		visible := map[string]bool{}
		for _, item := range m.visibleMenus() {
			visible[item.id] = true
		}
		var rows []menuItem
		for _, item := range m.menuDefinitions() {
			if !visible[item.id] {
				rows = append(rows, menuItem{item.label, "menu:" + item.id, true})
			}
		}
		return rows
	case "app":
		return []menuItem{{"Agent", "screen:agent", idle}, {"Workflows", "screen:workflows", idle}, sep,
			{"Change workspace…", "workspace", idle && m.application != nil}, sep, {"Quit", "quit", true}}
	case "session":
		saved := idle && m.stateFile != ""
		return append([]menuItem{{"New session", "/new", idle}, {"Open session…", "/sessions", true},
			{"Rename session…", "rename-current", saved}, {"Delete session…", "delete-current", saved}, sep,
			{"Compact history…", "compact-session", saved}, sep}, m.sessionCopyItems()...)
	case "execution":
		return []menuItem{{"Plan — read-only", "/mode plan", idle}, {"Act — allow changes", "/mode act", idle}, sep,
			{"Stop run", "stop", m.state == stRunning}, {"Retry failed turn", "/retry", idle && m.runErr != nil && m.retryRun != nil}, sep,
			{"Message queue…", "message-queue", true}, {"Background processes…", "/processes", true}, {"Apply new messages during run", "toggle-steering", true}}
	case "view":
		return []menuItem{{"Thinking", "toggle-details", true}, {"Tool calls", "toggle-tools", true}, {"System messages", "toggle-sys", true},
			{"Speaker names", "toggle-names", true}, {"Message labels", "toggle-labels", true}, sep, {"Jump to latest", "latest", true}}
	case "settings":
		editable := idle && m.config != nil
		section := func(id string) menuItem { return menuItem{settings.SectionTitle(id) + "…", "edit:" + id, editable} }
		rows := []menuItem{section("model"), section("generation"), section("profiles")}
		if m.config != nil {
			rows = append(rows, sep, section("you"), section("prompts"))
		}
		return append(rows, sep, menuItem{"Permissions…", "permissions-info", true}, menuItem{"MCP servers…", "/mcp", true}, sep, menuItem{"Appearance…", "appearance", true})
	case "help":
		rows := []menuItem{{"Keyboard shortcuts", "help-keys", true}, {"Commands", "help-commands", true}}
		if m.config != nil {
			rows = append(rows, menuItem{"Inspect agent instructions…", "prompt-info", true})
		}
		return append(rows, sep, menuItem{"About GEKTOR", "about", true})
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
	if m.workflows != nil {
		if toggle, on := m.workflows.MenuToggle(command); toggle {
			return toggle, on
		}
	}

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
		return true, !m.workflowActive()
	case "screen:workflows":
		return true, m.workflowActive()
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
	if key := workflowui.MenuShortcut(command); key != "" {
		return key
	}

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
