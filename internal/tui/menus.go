package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/tui/settings"
)

type menuItem struct {
	text, command string
	enabled       bool
}

type menuDefinition struct{ id, label string }

// menuDefinitions describes application navigation independently of header layout.
func (m *model) menuDefinitions() []menuDefinition {
	menus := []menuDefinition{{"app", "GEKTOR"}}
	if m.workflowActive() {
		menus = append(menus, menuDefinition{"recipe", "Recipe"}, menuDefinition{"node", "Node"})
	} else {
		menus = append(menus, menuDefinition{"session", "Session"})
	}
	return append(menus, menuDefinition{"execution", "Run"}, menuDefinition{"view", "View"}, menuDefinition{"settings", "Settings"}, menuDefinition{"help", "Help"})
}

// A narrow terminal keeps every menu reachable through More rather than clipping it.
func (m *model) visibleMenus() []menuDefinition {
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

func (m *model) menuItems(menu string) []menuItem {
	idle := m.state == stDone && (m.workflows == nil || !m.workflows.busy)
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
		return []menuItem{{"New session", "/new", idle}, {"Open session…", "/sessions", true},
			{"Rename session…", "rename-current", saved}, {"Delete session…", "delete-current", saved}, sep,
			{"Compact history…", "compact-session", saved}}
	case "recipe":
		if m.workflows == nil {
			return nil
		}
		edit := idle && m.workflows.run == nil
		return []menuItem{{"New recipe…", "/workflows new", idle}, {"Open recipe…", "/workflows open", idle},
			{"Save recipe…", "/workflows save", edit}, sep, {"Recipe settings…", "/workflows setup", edit}}
	case "node":
		if m.workflows == nil {
			return nil
		}
		edit := idle && m.workflows.run == nil
		return []menuItem{{"Add node…", "/workflows add", edit}, {"Add schedule trigger…", "/workflows schedule-add", edit}, sep,
			{"Edit arguments…", "/workflows edit", edit}, {"Behavior…", "/workflows behavior", edit},
			{"Connections…", "/workflows connections", edit}, {"Connect nodes…", "/workflows link", edit}, sep,
			{"Delete node…", "/workflows delete", edit}}
	case "execution":
		if !m.workflowActive() {
			return []menuItem{{"Plan — read-only", "/mode plan", idle}, {"Act — allow changes", "/mode act", idle}, sep,
				{"Stop run", "stop", m.state == stRunning}, {"Retry failed turn", "/retry", idle && m.runErr != nil && m.retryRun != nil}, sep,
				{"Message queue…", "message-queue", true}, {"Apply new messages during run", "toggle-steering", true}}
		}
		edit := idle && m.workflows.run == nil
		return []menuItem{{"Validate recipe…", "/workflows validate", edit}, {"Run recipe…", "/workflows run", edit},
			{"Stop run", "/workflows stop", m.workflows.busy}, sep, {"Run history…", "/workflows runs", idle},
			{"Review run…", "/workflows review", idle && m.workflows.run != nil}, {"Return to recipe", "/workflows draft", idle && m.workflows.run != nil}, sep,
			{"Set up schedule…", "/workflows schedule", edit}, {"Refresh schedule status", "/workflows schedule-refresh", idle}, sep,
			{"Run with JSON inputs…", "/workflows run-json", edit}}
	case "view":
		if m.workflowActive() {
			return []menuItem{{"Inspector", "/workflows inspector", true}, {"Raw inspection data", "/workflows raw-details", true}, sep,
				{"Lock node positions", "/workflows lock-positions", true}, {"Auto-layout", "/workflows layout", idle && !m.workflows.positionsLocked}}
		}
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
func (m *model) menuToggle(command string) (toggle, on bool) {
	switch command {
	case "/workflows raw-details":
		return m.workflows != nil, m.workflows != nil && m.workflows.rawDetails
	case "/workflows lock-positions":
		return m.workflows != nil, m.workflows != nil && m.workflows.positionsLocked
	case "/workflows inspector":
		return m.workflows != nil, m.workflows != nil && m.workflows.inspectorOpen
	case "/workflows overview":
		return m.workflows != nil, m.workflows != nil && m.workflows.overview
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
	switch command {
	case "/workflows raw-details":
		return "Ctrl+J"
	case "/workflows lock-positions":
		return "G"
	case "/workflows new":
		return "Ctrl+N"
	case "/workflows open":
		return "Ctrl+O"
	case "/workflows save":
		return "Ctrl+S"
	case "/workflows add":
		return "A"
	case "/workflows edit":
		return "Enter"
	case "/workflows behavior":
		return "E"
	case "/workflows connections":
		return "B"
	case "/workflows link":
		return "C"
	case "/workflows delete":
		return "Delete"
	case "/workflows validate":
		return "V"
	case "/workflows run":
		return "R"
	case "/workflows runs":
		return "H"
	case "/workflows review":
		return "P"
	case "/workflows draft":
		return "D"
	case "/workflows inspector":
		return "Ctrl+T"
	case "/workflows overview":
		return "F"
	case "/workflows layout":
		return "L"
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
	case "stop", "/workflows stop":
		return "Esc"
	case "/help", "help-keys":
		return "F1"
	}
	return ""
}
