package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

func (m *model) legacyFooterActions() []footerAction {
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

// The first three actions stay in place; the middle pair follows input ownership.
func (m *model) footerActions() []footerAction {
	if m.state == stPermission && m.dialog == nil && m.gstage == gsPermit {
		actions := []footerAction{{'y', 0, "Y", "Allow once", "Allow", true}, {'n', 0, "N", "Deny", "Deny", true}, {'a', 0, "A", "This run", "Run", true}}
		if m.gatePreview != "" {
			actions = append(actions, footerAction{tea.KeyF4, 0, "F4", "Proposal", "View", true})
		}
		return append(actions, footerAction{'q', tea.ModCtrl, "Ctrl+Q", "Quit", "Quit", true})
	}
	if m.dialog != nil && m.dialog.form != nil {
		if m.dialog.form.Picker != nil {
			return []footerAction{{tea.KeyEnter, 0, "Enter", "Choose", "Choose", true}, {tea.KeyEscape, 0, "Esc", "Back", "Back", true}, {'b', tea.ModCtrl, "Ctrl+B", "Browse", "Browse", pickerFiles(m.dialog.form.Picker.Field)}, {'r', tea.ModCtrl, "Ctrl+R", "Refresh", "Refresh", true}, {'q', tea.ModCtrl, "Ctrl+Q", "Quit", "Quit", true}}
		}
		apply := footerAction{'s', tea.ModCtrl, "Ctrl+S", "Apply", "Apply", true}
		if m.dialog.form.Destructive {
			apply = footerAction{tea.KeyEnter, 0, "Enter", "Choose", "Choose", true}
		}
		if ws := m.dialog.form.Workspace; ws != nil {
			// F6 was the in-form category switch. With one section per
			// modal there is nothing for it to switch, and a footer entry
			// that does nothing is worse than no entry.
			actions := []footerAction{{tea.KeyF4, 0, "F4", "Advanced", "More", true}, {tea.KeyTab, 0, "Tab", "Next", "Next", true}, apply, {tea.KeyEscape, 0, "Esc", "Cancel", "Cancel", true}}
			if ws.Section == "profiles" {
				actions = append(actions, footerAction{'d', tea.ModCtrl, "Ctrl+D", "Save copy", "Save", true})
			}
			return actions
		}
		return []footerAction{{tea.KeyTab, 0, "Tab", "Next", "Next", true}, apply, {tea.KeyEscape, 0, "Esc", "Cancel", "Cancel", true}, {'q', tea.ModCtrl, "Ctrl+Q", "Quit", "Quit", true}}
	}
	legacy := m.legacyFooterActions()
	actions := []footerAction{
		{tea.KeyF1, 0, "F1", "Help", "Help", legacy[0].enabled},
		{'p', tea.ModCtrl, "Ctrl+P", "Search", "Find", legacy[1].enabled},
		{'n', tea.ModCtrl, "Ctrl+N", "New", "New", legacy[3].enabled},
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
		footerAction{'g', tea.ModCtrl, "Ctrl+G", details, "View", legacy[2].enabled},
		footerAction{'q', tea.ModCtrl, "Ctrl+Q", "Quit", "Quit", true})
	if m.termW < 64 {
		return []footerAction{actions[0], actions[1], actions[3], actions[4], actions[len(actions)-1]}
	}
	return actions
}

func (m *model) actionBarVisible() bool { return m.termW >= 40 && m.termH >= 14 }
func (m *model) actionBarRows() int {
	if !m.actionBarVisible() {
		return 0
	}

	return 1
}

// The Commander-style action bar occupies exactly one row.
func (m *model) footerRows() int {
	if !m.actionBarVisible() {
		return 0
	}
	return m.actionBarRows() + 1
}

// Rendering and mouse clicks share button boundaries; gaps are not clickable.
func (m *model) footerCells() []footerCell {
	actions := m.footerActions()
	columns := (len(actions) + max(m.actionBarRows(), 1) - 1) / max(m.actionBarRows(), 1)
	width := m.paneWidth()
	cells := make([]footerCell, len(actions))
	for i, action := range actions {
		row, column := i/columns, i%columns
		count := min(columns, len(actions)-row*columns)
		contentWidth := width - (count - 1)
		start := m.promptOffset() + column*contentWidth/count + column
		end := m.promptOffset() + (column+1)*contentWidth/count + column
		cells[i] = footerCell{action, row, start, end}
	}
	return cells
}

func (m *model) actionBar() string {
	var rows []string
	row := ""
	current := 0
	gapStyle := lipgloss.NewStyle().Background(lipgloss.Color(currentPalette().background))
	for _, cell := range m.footerCells() {
		if cell.row != current {
			rows = append(rows, row)
			row = ""
			current = cell.row
		}

		label := cell.action.label
		number := cell.action.name
		if cell.end-cell.start < 14 && strings.HasPrefix(number, "Ctrl+") {
			number = "^" + strings.TrimPrefix(number, "Ctrl+")
		}
		if cell.end-cell.start < lipgloss.Width(number)+2+lipgloss.Width(label) {
			label = cell.action.short
		}
		numberStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorDisabledText))).Background(lipgloss.Color(themeColor(colorDisabledNumber)))
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorDisabledText))).Background(lipgloss.Color(themeColor(colorDisabledLabel)))
		if cell.action.enabled {
			numberStyle = numberStyle.Foreground(lipgloss.Color(themeColor(colorNumberText))).Background(lipgloss.Color(themeColor(colorNumberBackground))).Bold(true)
			labelStyle = labelStyle.Foreground(lipgloss.Color(themeColor(colorSelectedText))).Background(lipgloss.Color(themeColor(colorActionBackground)))
		}
		row += gapStyle.Render(strings.Repeat(" ", max(cell.start-lipgloss.Width(row), 0)))
		numberWidth := min(lipgloss.Width(number)+2, cell.end-cell.start)
		row += numberStyle.Render(cellLine(" "+number+" ", numberWidth)) + labelStyle.Render(cellLine(label, cell.end-cell.start-numberWidth))
	}
	row += gapStyle.Render(strings.Repeat(" ", max(m.termW-lipgloss.Width(row), 0)))
	return strings.Join(append(rows, row), "\n")
}

func (m *model) footerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	for _, action := range m.legacyFooterActions() {
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
func (m *model) requestQuit() (tea.Model, tea.Cmd) {
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
