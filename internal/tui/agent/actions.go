package agent

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/internal/tui/components/pane"
)

type footerAction struct {
	key                rune
	mod                tea.KeyMod
	name, label, short string
	enabled            bool
}

type footerCell struct {
	action          footerAction
	row, start, end int
}

func (m *Model) functionKeyActions() []footerAction {
	gated := m.state == stPermission || m.state == stAsk
	free := !gated && m.dialog == nil && m.sessions == nil && !m.header.focused
	idle := free && m.state == stDone
	return []footerAction{
		{tea.KeyF1, 0, "F1", "Help", "Help", !gated && m.sessions == nil && !m.header.focused},
		{tea.KeyF2, 0, "F2", "Search", "Search", !gated && m.dialog == nil && !m.header.focused},
		{tea.KeyF3, 0, "F3", "Thinking", "Think", free},
		{tea.KeyF4, 0, "F4", "New", "New", idle},
		{tea.KeyF10, 0, "F10", "Quit", "Quit", true},
	}
}

// Footer actions follow input ownership; the running composer keeps Queue visible.
func (m *Model) footerActions() []footerAction {
	if m.dialog != nil && m.dialog.table != nil {
		props := m.tableProps()

		usable := m.dialog.table.model.Layout(props).Capacity > 0
		enter := footerAction{tea.KeyEnter, 0, "Enter", "Choose", "Choose", false}
		for _, action := range props.Actions {
			if action.ID == props.DefaultAction {
				enter.label, enter.short, enter.enabled = action.Label, action.Label, action.Enabled
			}
		}
		if m.dialog.table.model.MoreOpen {
			return []footerAction{{tea.KeyEnter, 0, "Enter", "Choose", "Choose", true}, {tea.KeyEscape, 0, "Esc", "Back", "Back", true}, {'q', tea.ModCtrl, "Ctrl+Q", "Quit", "Quit", true}}
		}
		if !usable {
			enter = footerAction{tea.KeyEnter, 0, "Enter", "Close", "Close", true}
		}
		if props.List {
			return []footerAction{enter, {tea.KeyEscape, 0, "Esc", "Close", "Close", true}}
		}
		return []footerAction{{'/', 0, "/", "Filter", "Filter", usable}, {tea.KeyTab, 0, "Tab", "Controls", "Next", usable},
			enter,
			{tea.KeyEscape, 0, "Esc", "Back", "Back", true}, {'q', tea.ModCtrl, "Ctrl+Q", "Quit", "Quit", true}}
	}
	if m.state == stPermission && m.dialog == nil && m.gstage == gsPermit {
		actions := []footerAction{{'y', 0, "Y", "Allow once", "Allow", true}, {'n', 0, "N", "Deny", "Deny", true}, {'a', 0, "A", "This run", "Run", true}}
		if m.gatePreview != "" {
			actions = append(actions, footerAction{tea.KeyF4, 0, "F4", "Proposal", "View", true})
		}
		return append(actions, footerAction{'q', tea.ModCtrl, "Ctrl+Q", "Quit", "Quit", true})
	}
	if m.dialog != nil && m.dialog.form != nil {
		if m.dialog.form.Picker != nil {
			p := m.dialog.form.Picker
			label, enabled := p.EnterAction()
			enter := footerAction{tea.KeyEnter, 0, "Enter", label, label, enabled}
			if p.Field == "theme" {
				return []footerAction{enter, {tea.KeyEscape, 0, "Esc", "Back", "Back", true}}
			}
			if p.Field == "app-workspace" {
				return []footerAction{enter, {'s', tea.ModCtrl, "Ctrl+S", "Select folder", "Select", !p.Location && !p.Loading && p.Err == "" && p.Directory != ""}, {'l', tea.ModCtrl, "Ctrl+L", "Path", "Path", true}, {'d', tea.ModCtrl, "Ctrl+D", "Drives", "Drives", true}, {tea.KeyEscape, 0, "Esc", "Back", "Back", true}}
			}
			return []footerAction{enter, {tea.KeyEscape, 0, "Esc", "Back", "Back", true}, {'b', tea.ModCtrl, "Ctrl+B", "Browse", "Browse", m.dialog.form.Picker.IsFiles()}, {'r', tea.ModCtrl, "Ctrl+R", "Refresh", "Refresh", true}, {'q', tea.ModCtrl, "Ctrl+Q", "Quit", "Quit", true}}
		}
		label := m.dialog.form.ApplyLabel
		if label == "" {
			label = "Apply"
		}
		apply := footerAction{'s', tea.ModCtrl, "Ctrl+S", label, label, m.dialog.form.EditSession == nil || m.dialog.form.Dirty()}
		if m.dialog.form.Destructive {
			apply = footerAction{tea.KeyEnter, 0, "Enter", "Choose", "Choose", true}
		}
		if ws := m.dialog.form.EditSession; ws != nil {
			// F6 was the in-form category switch. With one section per
			// modal there is nothing for it to switch, and a footer entry
			// that does nothing is worse than no entry.
			actions := []footerAction{{tea.KeyF4, 0, "F4", "Advanced", "More", true}, {tea.KeyTab, 0, "Tab", "Next", "Next", true}, apply, {tea.KeyEscape, 0, "Esc", "Cancel", "Cancel", true}}
			if ws.Section == "profiles" {
				actions = append(actions, footerAction{'d', tea.ModCtrl, "Ctrl+D", "Save copy", "Save", true})
			}
			return actions
		}
		if m.termW < 64 {
			return []footerAction{apply, {tea.KeyEscape, 0, "Esc", "Cancel", "Cancel", true}}
		}
		return []footerAction{{tea.KeyTab, 0, "Tab", "Next", "Next", true}, apply, {tea.KeyEscape, 0, "Esc", "Cancel", "Cancel", true}, {'q', tea.ModCtrl, "Ctrl+Q", "Quit", "Quit", true}}
	}
	if m.workflowActive() && m.dialog == nil && m.sessions == nil && !m.header.focused {
		return m.workflowActions()
	}
	if m.state == stRunning && m.activeInputOwner() == focusInput {
		actions := []footerAction{{tea.KeyEscape, 0, "Esc", "Stop", "Stop", true}, {tea.KeyEnter, 0, "Enter", "Send", "Send", true}, {'q', tea.ModAlt, "Alt+Q", "Queue", "Queue", true}}
		if m.termW >= 64 && !m.transcript.Follow {
			actions = append(actions, footerAction{tea.KeyEnd, tea.ModCtrl, "Ctrl+End", "Latest", "End", true})
		} else if m.termW >= 64 || len(actions) < 3 {
			actions = append(actions, footerAction{tea.KeyF1, 0, "F1", "Help", "Help", true})
		}
		return actions
	}
	functionKeys := m.functionKeyActions()
	actions := []footerAction{
		{tea.KeyF1, 0, "F1", "Help", "Help", functionKeys[0].enabled},
		{'p', tea.ModCtrl, "Ctrl+P", "Search", "Find", functionKeys[1].enabled},
		{'n', tea.ModCtrl, "Ctrl+N", "New", "New", functionKeys[3].enabled},
	}
	switch m.activeInputOwner() {
	case focusSearch:
		editable := m.state == stDone && (m.sessions == nil || m.sessions.mode == sessList)
		actions = append(actions, footerAction{'r', tea.ModCtrl, "Ctrl+R", "Rename", "Name", editable}, footerAction{'d', tea.ModCtrl, "Ctrl+D", "Delete", "Del", editable})
	case focusInput:
		if m.state == stRunning || !m.transcript.Follow {
			actions = append(actions, footerAction{tea.KeyEscape, 0, "Esc", "Stop", "Stop", m.state == stRunning}, footerAction{tea.KeyEnd, tea.ModCtrl, "Ctrl+End", "Latest", "End", true})
		} else {
			actions = append(actions, footerAction{tea.KeyEnter, 0, "Enter", "Send", "Go", m.state == stDone || m.state == stAsk}, footerAction{'o', tea.ModCtrl, "Ctrl+O", "Newline", "Line", true})
		}
	default:
		actions = append(actions, footerAction{tea.KeyEscape, 0, "Esc", "Back", "Back", m.state != stPermission}, footerAction{tea.KeyEnter, 0, "Enter", "Choose", "OK", m.activeInputOwner() == focusHeader || m.activeInputOwner() == focusNote})
	}
	if m.activeInputOwner() == focusInput && m.state == stDone && !m.transcript.Follow {
		actions[3] = footerAction{tea.KeyEnter, 0, "Enter", "Send", "Go", true}
	}
	details := "Thinking"
	actions = append(actions,
		footerAction{'g', tea.ModCtrl, "Ctrl+G", details, "View", functionKeys[2].enabled},
		footerAction{'q', tea.ModCtrl, "Ctrl+Q", "Quit", "Quit", true})
	if m.termW < 64 {
		return []footerAction{actions[0], actions[1], actions[3], actions[4], actions[len(actions)-1]}
	}
	return actions
}

func (m *Model) actionBarVisible() bool { return m.termW >= 40 && m.termH >= 14 }
func (m *Model) actionBarRows() int {
	if !m.actionBarVisible() {
		return 0
	}

	return 1
}

// The Commander-style action bar occupies exactly one row.
func (m *Model) footerRows() int {
	if !m.actionBarVisible() {
		return 0
	}
	return m.actionBarRows() + 1
}

// Rendering and mouse clicks use the component's measured action cells.
func (m *Model) toolbarLayout() pane.ToolbarLayout {
	var items []pane.ToolAction
	for _, action := range m.footerActions() {
		compactKey := ""
		if strings.HasPrefix(action.name, "Ctrl+") {
			compactKey = "^" + strings.TrimPrefix(action.name, "Ctrl+")
		}
		items = append(items, pane.ToolAction{Key: action.name, CompactKey: compactKey, Label: action.label, CompactLabel: action.short, Enabled: action.enabled})
	}
	style := func(fg, bg string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(fg))).Background(lipgloss.Color(themeColor(bg)))
	}
	return pane.Toolbar(m.termW, items, pane.ToolbarStyles{
		Key:           style(colorNumberText, colorNumberBackground).Bold(true),
		Label:         style(colorSelectedText, colorActionBackground),
		DisabledKey:   style(colorDisabledText, colorDisabledNumber),
		DisabledLabel: style(colorDisabledText, colorDisabledLabel),
		Gap:           lipgloss.NewStyle().Background(lipgloss.Color(currentPalette().background)),
	})
}

func (m *Model) footerCells() []footerCell {
	actions := m.footerActions()
	var cells []footerCell
	for i, item := range m.toolbarLayout().Cells {
		cells = append(cells, footerCell{action: actions[i], row: item.Bounds.Y, start: item.Bounds.X, end: item.Bounds.X + item.Bounds.Width})
	}
	return cells
}

func (m *Model) actionBar() string { return m.toolbarLayout().Row }

func (m *Model) toolbarClick(x int) (tea.Model, tea.Cmd) {
	if index := m.toolbarLayout().Hit(x, 0); index >= 0 {
		action := m.footerActions()[index]
		return m.handleKey(tea.KeyPressMsg{Code: action.key, Mod: action.mod})
	}
	return m, nil
}

func (m *Model) footerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	for _, action := range m.functionKeyActions() {
		if action.key == msg.Code && !action.enabled {
			return m, nil
		}
	}
	switch msg.Code {
	case tea.KeyF1:
		if m.dialog != nil {
			m.closeDialog()
		} else {
			m.openHelp()
		}
	case tea.KeyF2:
		if m.sessions != nil {
			m.closeSessions()
		} else {
			m.openSessions()
		}
	case tea.KeyF3:
		return m.toggleThinking()
	case tea.KeyF4:
		m.newTask()
		return m, m.input.Focus()
	case tea.KeyF5:
		gated := m.state == stPermission || m.state == stAsk
		free := !gated && m.dialog == nil && m.sessions == nil && !m.header.focused
		idle := free && m.state == stDone
		if !idle {
			return m, nil
		}
		mode := "plan"
		if m.plan {
			mode = "act"
		}
		return m.command("/mode " + mode)
	case tea.KeyF7:
		gated := m.state == stPermission || m.state == stAsk
		free := !gated && m.dialog == nil && m.sessions == nil && !m.header.focused
		if !free {
			return m, nil
		}
		m.transcript.Viewport.GotoBottom()
		m.transcript.Follow = true
	case tea.KeyF9:
		gated := m.state == stPermission || m.state == stAsk
		if !(!gated && m.dialog == nil && m.sessions == nil) {
			return m, nil
		}
		m.header.focused = !m.header.focused
		m.header.global = m.header.focused
		m.header.index = 0
		m.header.menu = ""
	case tea.KeyF10:
		return m.requestQuit()
	}
	return m, nil
}

// A normal quit first stops an active run. Ctrl+C remains the immediate exit.
func (m *Model) requestQuit() (tea.Model, tea.Cmd) {
	if w := m.workflows; w != nil {
		if !w.PrepareQuit() {
			return m, nil
		}
		if w.Dirty() {
			m.openForm("Quit with unsaved workflow?", "Return to Workflows and save, or discard the draft.", "Quit", []formField{{Key: "decision", Label: "Type discard to quit"}}, func(v map[string]string) error {
				if v["decision"] != "discard" {
					return fmt.Errorf("type discard, or Esc to keep editing")
				}
				m.workflows.Close()
				m.workflows = nil
				m.pendingCommands = append(m.pendingCommands, func() tea.Msg { return workflowQuit{} })
				return nil
			})
			return m, nil
		}
	}

	if m.state != stDone {
		if m.state != stStopping {
			m.interrupted = true
			m.cancel()
			m.state = stStopping
			m.appendBlock(markerBlock("Stopping run. Ctrl+Q quits once it has stopped; Ctrl+C exits immediately."))
			m.fitBottom()
		}
		return m, nil
	}
	m.quit = true
	m.cancel()
	return m, tea.Quit
}
