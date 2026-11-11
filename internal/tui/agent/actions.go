package agent

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/screen"
)

func (m *Model) functionKeyActions() []screen.Action {
	gated := m.state == stPermission || m.state == stAsk
	free := !gated && m.chrome.Dialog == nil && m.sessions == nil && !m.chrome.Header.Focused
	idle := free && m.state == stDone
	return []screen.Action{
		{Key: tea.KeyF1, Mod: 0, Name: "F1", Label: "Help", Short: "Help", Enabled: !gated && m.sessions == nil && !m.chrome.Header.Focused},
		{Key: tea.KeyF2, Mod: 0, Name: "F2", Label: "Search", Short: "Search", Enabled: !gated && m.chrome.Dialog == nil && !m.chrome.Header.Focused},
		{Key: tea.KeyF3, Mod: 0, Name: "F3", Label: "Thinking", Short: "Think", Enabled: free},
		{Key: tea.KeyF4, Mod: 0, Name: "F4", Label: "New", Short: "New", Enabled: idle},
		{Key: tea.KeyF10, Mod: 0, Name: "F10", Label: "Quit", Short: "Quit", Enabled: true},
	}
}

// Footer actions follow input ownership; the running composer keeps Queue visible.
func (m *Model) footerActions() []screen.Action {
	if m.chrome.Dialog != nil && (m.chrome.Dialog.Form != nil || m.chrome.Dialog.Table != nil) {
		return m.chrome.DialogActions()
	}

	if m.state == stPermission && m.chrome.Dialog == nil && m.gstage == gsPermit {
		actions := []screen.Action{{Key: 'y', Mod: 0, Name: "Y", Label: "Allow once", Short: "Allow", Enabled: true}, {Key: 'n', Mod: 0, Name: "N", Label: "Deny", Short: "Deny", Enabled: true}, {Key: 'a', Mod: 0, Name: "A", Label: "This run", Short: "Run", Enabled: true}}
		if m.gatePreview != "" {
			actions = append(actions, screen.Action{Key: tea.KeyF4, Mod: 0, Name: "F4", Label: "Proposal", Short: "View", Enabled: true})
		}
		return append(actions, screen.Action{Key: 'q', Mod: tea.ModCtrl, Name: "Ctrl+Q", Label: "Quit", Short: "Quit", Enabled: true})
	}

	if m.state == stRunning && m.activeInputOwner() == focusInput {
		actions := []screen.Action{{Key: tea.KeyEscape, Mod: 0, Name: "Esc", Label: "Stop", Short: "Stop", Enabled: true}, {Key: tea.KeyEnter, Mod: 0, Name: "Enter", Label: "Send", Short: "Send", Enabled: true}, {Key: 'q', Mod: tea.ModAlt, Name: "Alt+Q", Label: "Queue", Short: "Queue", Enabled: true}}
		if m.termW >= 64 && !m.transcript.Follow {
			actions = append(actions, screen.Action{Key: tea.KeyEnd, Mod: tea.ModCtrl, Name: "Ctrl+End", Label: "Latest", Short: "End", Enabled: true})
		} else if m.termW >= 64 || len(actions) < 3 {
			actions = append(actions, screen.Action{Key: tea.KeyF1, Mod: 0, Name: "F1", Label: "Help", Short: "Help", Enabled: true})
		}
		return actions
	}
	functionKeys := m.functionKeyActions()
	actions := []screen.Action{
		{Key: tea.KeyF1, Mod: 0, Name: "F1", Label: "Help", Short: "Help", Enabled: functionKeys[0].Enabled},
		{Key: 'p', Mod: tea.ModCtrl, Name: "Ctrl+P", Label: "Search", Short: "Find", Enabled: functionKeys[1].Enabled},
		{Key: 'n', Mod: tea.ModCtrl, Name: "Ctrl+N", Label: "New", Short: "New", Enabled: functionKeys[3].Enabled},
	}
	switch m.activeInputOwner() {
	case focusSearch:
		editable := m.state == stDone && (m.sessions == nil || m.sessions.mode == sessList)
		actions = append(actions, screen.Action{Key: 'r', Mod: tea.ModCtrl, Name: "Ctrl+R", Label: "Rename", Short: "Name", Enabled: editable}, screen.Action{Key: 'd', Mod: tea.ModCtrl, Name: "Ctrl+D", Label: "Delete", Short: "Del", Enabled: editable})
	case focusInput:
		if m.state == stRunning || !m.transcript.Follow {
			actions = append(actions, screen.Action{Key: tea.KeyEscape, Mod: 0, Name: "Esc", Label: "Stop", Short: "Stop", Enabled: m.state == stRunning}, screen.Action{Key: tea.KeyEnd, Mod: tea.ModCtrl, Name: "Ctrl+End", Label: "Latest", Short: "End", Enabled: true})
		} else {
			actions = append(actions, screen.Action{Key: tea.KeyEnter, Mod: 0, Name: "Enter", Label: "Send", Short: "Go", Enabled: m.state == stDone || m.state == stAsk}, screen.Action{Key: 'o', Mod: tea.ModCtrl, Name: "Ctrl+O", Label: "Newline", Short: "Line", Enabled: true})
		}
	default:
		actions = append(actions, screen.Action{Key: tea.KeyEscape, Mod: 0, Name: "Esc", Label: "Back", Short: "Back", Enabled: m.state != stPermission}, screen.Action{Key: tea.KeyEnter, Mod: 0, Name: "Enter", Label: "Choose", Short: "OK", Enabled: m.activeInputOwner() == focusHeader || m.activeInputOwner() == focusNote})
	}
	if m.activeInputOwner() == focusInput && m.state == stDone && !m.transcript.Follow {
		actions[3] = screen.Action{Key: tea.KeyEnter, Mod: 0, Name: "Enter", Label: "Send", Short: "Go", Enabled: true}
	}
	details := "Thinking"
	actions = append(actions,
		screen.Action{Key: 'g', Mod: tea.ModCtrl, Name: "Ctrl+G", Label: details, Short: "View", Enabled: functionKeys[2].Enabled},
		screen.Action{Key: 'q', Mod: tea.ModCtrl, Name: "Ctrl+Q", Label: "Quit", Short: "Quit", Enabled: true})
	if m.termW < 64 {
		return []screen.Action{actions[0], actions[1], actions[3], actions[4], actions[len(actions)-1]}
	}
	return actions
}

func (m *Model) actionBarVisible() bool {
	m.chrome.Props.Width, m.chrome.Props.Height = m.termW, m.termH
	return m.chrome.ActionBarVisible()
}

func (m *Model) actionBarRows() int {
	m.chrome.Props.Width, m.chrome.Props.Height = m.termW, m.termH
	return m.chrome.ActionBarRows()
}

// The Commander-style action bar occupies exactly one row.
func (m *Model) footerRows() int {
	m.chrome.Props.Width, m.chrome.Props.Height = m.termW, m.termH
	return m.chrome.FooterRows()
}

func (m *Model) toolbarClick(x int) (tea.Model, tea.Cmd) {
	if index := m.chrome.ToolbarLayout().Hit(x, 0); index >= 0 {
		action := m.footerActions()[index]
		return m.handleKey(tea.KeyPressMsg{Code: action.Key, Mod: action.Mod})
	}
	return m, nil
}

func (m *Model) footerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	for _, action := range m.functionKeyActions() {
		if action.Key == msg.Code && !action.Enabled {
			return m, nil
		}
	}
	switch msg.Code {
	case tea.KeyF1:
		if m.chrome.Dialog != nil {
			m.chrome.Close()
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
		free := !gated && m.chrome.Dialog == nil && m.sessions == nil && !m.chrome.Header.Focused
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
		free := !gated && m.chrome.Dialog == nil && m.sessions == nil && !m.chrome.Header.Focused
		if !free {
			return m, nil
		}
		m.transcript.Viewport.GotoBottom()
		m.transcript.Follow = true
	case tea.KeyF9:
		gated := m.state == stPermission || m.state == stAsk
		if !(!gated && m.chrome.Dialog == nil && m.sessions == nil) {
			return m, nil
		}
		m.chrome.Header.Focused = !m.chrome.Header.Focused
		m.chrome.Header.Global = m.chrome.Header.Focused
		m.chrome.Header.Index = 0
		m.chrome.Header.Menu = ""
	case tea.KeyF10:
		return m.requestQuit()
	}
	return m, nil
}

// A normal quit first stops an active run. Ctrl+C remains the immediate exit.
func (m *Model) requestQuit() (tea.Model, tea.Cmd) { return m, m.request(screen.QuitRequest{}) }

// PrepareQuit stops an active turn before the shell exits. A second quit after
// completion succeeds, preserving the operator's existing stop-first behavior.
func (m *Model) PrepareQuit() bool {
	if m.state != stDone {
		if m.state != stStopping {
			m.interrupted = true
			m.cancel()
			m.state = stStopping
			m.appendBlock(markerBlock("Stopping run. Ctrl+Q quits once it has stopped; Ctrl+C exits immediately."))
			m.fitBottom()
		}
		return false
	}
	return true
}
