package tui

import tea "charm.land/bubbletea/v2"

func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.dialog != nil && m.dialog.form != nil {
		if msg.Mouse().Button == tea.MouseLeft && isMouseClick(msg) && msg.Mouse().Y == m.termH-1 {
			for _, cell := range m.footerCells() {
				if msg.Mouse().X >= cell.start && msg.Mouse().X < cell.end {
					return m.handleKey(tea.KeyPressMsg{Code: cell.action.key, Mod: cell.action.mod})
				}
			}
		}
		return m, m.formMouse(msg)
	}
	if handled, cmd := m.headerMouse(msg); handled {
		return m, cmd
	}
	if m.actionBarVisible() && msg.Mouse().Y >= m.termH-m.actionBarRows() && msg.Mouse().Y < m.termH && msg.Mouse().Button == tea.MouseLeft && isMouseClick(msg) {
		for _, cell := range m.footerCells() {
			if msg.Mouse().Y == m.termH-m.actionBarRows()+cell.row && msg.Mouse().X >= cell.start && msg.Mouse().X < cell.end {
				if !cell.action.enabled {
					return m, nil
				}
				return m.handleKey(tea.KeyPressMsg{Code: cell.action.key, Mod: cell.action.mod})
			}
		}
		return m, nil
	}
	if m.state == stPermission || m.state == stAsk {
		if msg.Mouse().X < m.promptOffset() || msg.Mouse().Y < paneTop || msg.Mouse().Y >= paneTop+m.transcript.Viewport.Height() {
			return m, nil
		}
		var cmd tea.Cmd
		m.gateVP, cmd = m.gateVP.Update(msg)
		return m, cmd
	}
	if m.sessions != nil {
		return m.browserMouse(msg)
	}
	if m.sidebarVisible() && msg.Mouse().X < navigatorWidth && msg.Mouse().Y >= paneTop && msg.Mouse().Y < paneTop+m.paneHeight() {
		if m.state == stPermission || m.state == stAsk {
			return m, nil
		}
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			m.nav.Cursor--
		case tea.MouseWheelDown:
			m.nav.Cursor++
		case tea.MouseLeft:
			if !isMouseClick(msg) {
				return m, nil
			}
			row := msg.Mouse().Y - paneTop
			if msg.Mouse().Y < paneTop || row >= m.navigatorRows() || m.nav.Offset+row >= len(m.nav.entries) {
				return m, nil
			}
			m.nav.Cursor = m.nav.Offset + row
		default:
			return m, nil
		}
		m.nav.clamp(m.navigatorRows())
		m.navFocused = true
		m.input.Blur()
		return m, nil
	}
	if m.dialog == nil && m.sessions == nil && m.sidebarVisible() && msg.Mouse().X >= navigatorWidth+3 && msg.Mouse().Y >= paneTop && msg.Mouse().Y < paneTop+m.paneHeight() && msg.Mouse().Button == tea.MouseLeft && isMouseClick(msg) {
		m.navFocused = false
		return m, m.input.Focus()
	}
	if m.dialog != nil {
		switch msg.Mouse().Button {
		case tea.MouseWheelDown:
			m.dialog.offset++
		case tea.MouseWheelUp:
			m.dialog.offset = max(m.dialog.offset-1, 0)
		}
		return m, nil
	}
	// An overlay freezes the background: scroll resumes on close.
	if m.dialog != nil || m.sessions != nil {
		return m, nil
	}
	// The input never consumes mouse messages, so scroll works in
	// every state: wheel in ask/done used to fall through and die.
	return m.scrollViewport(msg)
}
