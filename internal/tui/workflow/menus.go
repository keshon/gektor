package workflow

import "github.com/keshon/gektor/internal/tui/screen"

// Menus lists the workflow-specific command groups in navigation order.
func Menus() []screen.Menu {
	return []screen.Menu{{ID: "recipe", Label: "Recipe"}, {ID: "node", Label: "Node"}}
}

// MenuItems supplies recipe, node, execution, and view commands. applicationIdle
// reports whether other application work permits editing and execution.
// A nil result leaves the menu to the application host.
func (m *Model) MenuItems(menu string, applicationIdle bool) []screen.MenuItem {
	idle := applicationIdle && !m.busy
	edit := idle && m.run == nil
	sep := screen.MenuItem{}
	switch menu {
	case "recipe":
		return []screen.MenuItem{
			{Text: "New recipe…", Command: "/workflows new", Enabled: idle},
			{Text: "Open recipe…", Command: "/workflows open", Enabled: idle},
			{Text: "Save recipe…", Command: "/workflows save", Enabled: edit}, sep,
			{Text: "Recipe settings…", Command: "/workflows setup", Enabled: edit},
		}
	case "node":
		return []screen.MenuItem{
			{Text: "Add node…", Command: "/workflows add", Enabled: edit},
			{Text: "Add schedule trigger…", Command: "/workflows schedule-add", Enabled: edit}, sep,
			{Text: "Edit arguments…", Command: "/workflows edit", Enabled: edit},
			{Text: "Behavior…", Command: "/workflows behavior", Enabled: edit},
			{Text: "Connections…", Command: "/workflows connections", Enabled: edit},
			{Text: "Connect nodes…", Command: "/workflows link", Enabled: edit}, sep,
			{Text: "Delete node…", Command: "/workflows delete", Enabled: edit},
		}
	case "execution":
		return []screen.MenuItem{
			{Text: "Validate recipe…", Command: "/workflows validate", Enabled: edit},
			{Text: "Run recipe…", Command: "/workflows run", Enabled: edit},
			{Text: "Stop run", Command: "/workflows stop", Enabled: m.busy}, sep,
			{Text: "Run history…", Command: "/workflows runs", Enabled: idle},
			{Text: "Review run…", Command: "/workflows review", Enabled: idle && m.run != nil},
			{Text: "Return to recipe", Command: "/workflows draft", Enabled: idle && m.run != nil}, sep,
			{Text: "Set up schedule…", Command: "/workflows schedule", Enabled: edit},
			{Text: "Refresh schedule status", Command: "/workflows schedule-refresh", Enabled: idle}, sep,
			{Text: "Run with JSON inputs…", Command: "/workflows run-json", Enabled: edit}, sep,
			{Text: "Background processes…", Command: "/processes", Enabled: true},
		}
	case "view":
		return []screen.MenuItem{
			{Text: "Inspector", Command: "/workflows inspector", Enabled: true},
			{Text: "Raw inspection data", Command: "/workflows raw-details", Enabled: true}, sep,
			{Text: "Lock node positions", Command: "/workflows lock-positions", Enabled: true},
			{Text: "Auto-layout", Command: "/workflows layout", Enabled: idle && !m.positionsLocked},
		}
	}
	return nil
}

// MenuToggle reports whether a workflow command is checkable and currently on.
func (m *Model) MenuToggle(command string) (toggle, on bool) {
	switch command {
	case "/workflows raw-details":
		return true, m.rawDetails
	case "/workflows lock-positions":
		return true, m.positionsLocked
	case "/workflows inspector":
		return true, m.inspectorOpen
	case "/workflows overview":
		return true, m.overview
	}
	return false, false
}

// MenuShortcut names the direct key for a workflow command. An empty result
// means there is no shortcut to display beside that command.
func MenuShortcut(command string) string {
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
	case "/workflows stop":
		return "Esc"
	}
	return ""
}
