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
	if m.scrollbarMouse(msg) {
		return m, nil
	}
	// The input never consumes mouse messages, so scroll works in
	// every state: wheel in ask/done used to fall through and die.
	return m.scrollViewport(msg)
}

	// scrollDrag is a scrollbar thumb grab: which row of the thumb the
// pointer holds, and whether it has moved since the grab.
type scrollDrag struct {
	active bool
	grab   int
	moved  bool
}

// scrollbarHit reports whether the pointer is on the transcript
// scrollbar, and which body row it points at. The bar lives in the last
// column of the transcript pane, so anything left of it is content.
func (m *model) scrollbarHit(x, y int) (row int, ok bool) {
	if !m.ready || !m.transcript.Viewport.Scrollable() {
		return 0, false
	}
	left := m.promptOffset()
	bar := left + m.transcript.Viewport.Width() - 1
	if m.sidebarVisible() {
		bar += navigatorWidth + 3
	}
	if x != bar {
		return 0, false
	}
	top := paneTop
	row = y - top
	if row < 0 || row >= m.transcript.Viewport.Height() {
		return 0, false
	}
	return row, true
}

// scrollbarMouse handles press, motion and release on the transcript
// scrollbar. A grab on the thumb drags it; a click on the track pages.
// It reports whether it consumed the message.
func (m *model) scrollbarMouse(msg tea.MouseMsg) bool {
	mouse := msg.Mouse()
	switch msg.(type) {
	case tea.MouseMotionMsg:
		if !m.scrollDrag.active {
			return false
		}
		row, ok := m.scrollbarHit(mouse.X, mouse.Y)
		if !ok {
			// Clamp to the nearest end: dragging past the edge pins
			// the document there instead of stalling mid-drag.
			if mouse.X == m.scrollbarX() {
				row = 0
				if mouse.Y >= paneTop {
					row = m.transcript.Viewport.Height() - 1
				}
			} else {
				return false
			}
		}
		m.scrollDrag.moved = true
		m.transcript.Viewport.SetYOffset(m.transcript.Viewport.DragOffset(row, m.transcript.Viewport.Height(), m.scrollDrag.grab))
		m.transcript.Follow = m.transcript.Viewport.AtBottom()
		return true
	case tea.MouseReleaseMsg:
		if !m.scrollDrag.active {
			return false
		}
		drag := m.scrollDrag
		m.scrollDrag = scrollDrag{}
		if !drag.moved && mouse.Button == tea.MouseLeft {
			// A press-and-release without motion is a click: thumb
			// holds position, track pages toward the pointer.
			top, size := m.transcript.Viewport.ThumbRange(m.transcript.Viewport.Height())
			row, ok := m.scrollbarHit(mouse.X, mouse.Y)
			if !ok {
				return true
			}
			switch {
			case row < top:
				m.transcript.Viewport.PageUp()
			case row >= top+size:
				m.transcript.Viewport.PageDown()
			}
			m.transcript.Follow = m.transcript.Viewport.AtBottom()
		}
		return true
	}
	if mouse.Button != tea.MouseLeft || !isMouseClick(msg) {
		return false
	}
	row, ok := m.scrollbarHit(mouse.X, mouse.Y)
	if !ok {
		// A release off the bar still ends a grab: the pointer can
		// wander mid-drag, and the grab must not stick.
		if _, isRelease := msg.(tea.MouseReleaseMsg); isRelease {
			m.scrollDrag = scrollDrag{}
			return true
		}
		return false
	}
	top, _ := m.transcript.Viewport.ThumbRange(m.transcript.Viewport.Height())
	m.scrollDrag = scrollDrag{active: true, grab: max(row-top, 0)}
	return true
}

// scrollbarX is the terminal column of the scrollbar, or -1 when the
// bar is not drawn.
func (m *model) scrollbarX() int {
	if !m.ready || !m.transcript.Viewport.Scrollable() {
		return -1
	}
	x := m.promptOffset() + m.transcript.Viewport.Width() - 1
	if m.sidebarVisible() {
		x += navigatorWidth + 3
	}
	return x
}
