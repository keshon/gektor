package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/transcript"
)

func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.workflowActive() && m.dialog == nil && (m.workflows.dragging || m.workflows.panning || m.workflows.thumb.active) {
		return m.workflowMouse(msg)
	}
	if m.sessions != nil {
		return m.browserMouse(msg)
	}
	if m.dialog != nil && m.dialog.table != nil {
		if msg.Mouse().Button == tea.MouseLeft && isMouseClick(msg) && msg.Mouse().Y == m.termH-1 {
			for _, cell := range m.footerCells() {
				if msg.Mouse().X >= cell.start && msg.Mouse().X < cell.end && cell.action.enabled {
					return m.handleKey(tea.KeyPressMsg{Code: cell.action.key, Mod: cell.action.mod})
				}
			}
		}
		left, top, _, _ := m.settingsArea()
		mouse := msg.Mouse()
		mouse.X -= left
		mouse.Y -= top
		var local tea.MouseMsg
		switch msg.(type) {
		case tea.MouseClickMsg:
			local = tea.MouseClickMsg(mouse)
		case tea.MouseWheelMsg:
			local = tea.MouseWheelMsg(mouse)
		default:
			return m, nil
		}
		action := m.dialog.table.model.Mouse(local, m.tableProps())
		return m, m.tableAction(action)
	}
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
	if m.inspectorVisible() && m.header.menu == "" && m.inspector.drag.active && m.inspectorScrollbarMouse(msg) {
		return m, nil
	}
	// A grabbed thumb owns motion and release until the button is released,
	// including over the footer and outside the scrollbar column.
	if m.scrollDrag.active && m.dialog == nil && m.sessions == nil && m.header.menu == "" && m.scrollbarMouse(msg) {
		return m, nil
	}
	if handled, cmd := m.headerMouse(msg); handled {
		return m, cmd
	}
	if m.workflowActive() && m.dialog == nil {
		return m.workflowMouse(msg)
	}
	if m.inspectorVisible() && (m.termW < inspectorSplitWidth || msg.Mouse().X >= m.mainWidth()) && msg.Mouse().Y >= 1 && msg.Mouse().Y < m.termH-m.actionBarRows() {
		if handled, cmd := m.inspectorMouse(msg); handled {
			return m, cmd
		}
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
	if m.dialog != nil {
		if m.dialog.form == nil && msg.Mouse().Button == tea.MouseLeft && isMouseClick(msg) {
			rows, w, h := m.dialogRows()
			inner := popup.ModalWidth(w) - 4
			left, _, _, _ := m.settingsArea()
			x := left + max((w-popup.ModalWidth(w))/2, 0) + 2
			y := paneTop + max((h-len(rows)-2)/2, 0) + 1
			if msg.Mouse().Y == y && msg.Mouse().X >= x+inner-3 && msg.Mouse().X < x+inner {
				m.closeDialog()
				return m, nil
			}
		}
		if m.dialogScrollbarMouse(msg) {
			return m, nil
		}
		switch msg.Mouse().Button {
		case tea.MouseWheelDown:
			m.scrollDialog(1)
		case tea.MouseWheelUp:
			m.scrollDialog(-1)
		}
		return m, nil
	}
	if m.state == stPermission || m.state == stAsk {
		mouse := msg.Mouse()
		if mouse.X < m.promptOffset() || mouse.Y < paneTop || mouse.Y >= paneTop+m.transcript.Viewport.Height() {
			return m, nil
		}
		// The gate popup overlays the transcript rather than replacing it,
		// so the transcript is what is on screen and what the wheel has to
		// move. gateVP still backs the narrow terminals where the popup
		// does not fit and the gate takes the pane outright.
		if m.gatePopup.Pending() {
			if m.scrollbarMouse(msg) {
				return m, nil
			}
			return m.scrollViewport(msg)
		}
		var cmd tea.Cmd
		m.gateVP, cmd = m.gateVP.Update(msg)
		return m, cmd
	}

	if handled, cmd := m.inspectorMouse(msg); handled {
		return m, cmd
	}
	if m.scrollbarMouse(msg) {
		return m, nil
	}

	// The input never consumes mouse messages, so scroll works in
	// every state: wheel in ask/done used to fall through and die.
	return m.scrollViewport(msg)
}

func (m *model) dialogScrollbarMouse(msg tea.MouseMsg) bool {
	d := m.dialog
	if d == nil || d.form != nil {
		return false
	}
	l := m.layoutDialog()
	if len(l.lines) <= l.capacity {
		return false
	}
	rows, _, _ := m.dialogRows()
	left, _, _, _ := m.settingsArea()
	x := left + max((l.width-popup.ModalWidth(l.width))/2, 0) + 2 + l.inner - 1
	y := paneTop + max((l.height-len(rows)-2)/2, 0) + 1 + len(l.rows)
	mouse := msg.Mouse()
	if d.drag.active {
		switch msg.(type) {
		case tea.MouseMotionMsg, tea.MouseReleaseMsg:
			d.offset = popup.ScrollOffset(mouse.Y-y, l.capacity, len(l.lines), d.drag.grab)
			if _, ok := msg.(tea.MouseReleaseMsg); ok {
				d.drag = scrollDrag{}
			}
			return true
		}
	}
	if mouse.X != x || mouse.Y < y || mouse.Y >= y+l.capacity || mouse.Button != tea.MouseLeft || !isMouseClick(msg) {
		return false
	}
	top, size := popup.ScrollThumb(l.capacity, len(l.lines), d.offset)
	row := mouse.Y - y
	if row >= top && row < top+size {
		d.drag = scrollDrag{active: true, grab: row - top}
	} else {
		m.scrollDialog(map[bool]int{true: -l.capacity, false: l.capacity}[row < top])
	}
	return true
}

// scrollDrag is a scrollbar thumb grab: which row of the thumb the
// pointer holds, and whether it has moved since the grab.
type scrollDrag struct {
	active bool
	grab   int
	moved  bool
}

// scrollbarMouse handles press, motion and release on the transcript
// scrollbar. A grab on the thumb drags it; a click on the track pages.
// It reports whether it consumed the message.
func (m *model) scrollbarMouse(msg tea.MouseMsg) bool {
	return viewportScrollbarMouse(&m.transcript.Viewport, &m.transcript.Follow, &m.scrollDrag, m.scrollbarX(), paneTop, msg)
}

func viewportScrollbarMouse(v *transcript.Viewport, follow *bool, drag *scrollDrag, column, paneY int, msg tea.MouseMsg) bool {
	hit := func(x, y int) (int, bool) {
		row := y - paneY
		return row, v.Scrollable() && column >= 0 && x == column && row >= 0 && row < v.Height()
	}
	mouse := msg.Mouse()
	switch msg.(type) {
	case tea.MouseMotionMsg:
		if !drag.active {
			return false
		}
		row := min(max(mouse.Y-paneY, 0), v.Height()-1)
		drag.moved = true
		v.SetYOffset(v.DragOffset(row, v.Height(), drag.grab))
		*follow = v.AtBottom()
		return true
	case tea.MouseReleaseMsg:
		if !drag.active {
			return false
		}
		previous := *drag
		*drag = scrollDrag{}
		if !previous.moved && mouse.Button == tea.MouseLeft {
			// A press-and-release without motion is a click: thumb
			// holds position, track pages toward the pointer.
			top, size := v.ThumbRange(v.Height())
			row, ok := hit(mouse.X, mouse.Y)
			if !ok {
				return true
			}
			switch {
			case row < top:
				v.PageUp()
			case row >= top+size:
				v.PageDown()
			}
			*follow = v.AtBottom()
		}
		return true
	}
	if mouse.Button != tea.MouseLeft || !isMouseClick(msg) {
		return false
	}
	row, ok := hit(mouse.X, mouse.Y)
	if !ok {
		return false
	}
	top, _ := v.ThumbRange(v.Height())
	*drag = scrollDrag{active: true, grab: max(row-top, 0)}
	return true
}

// scrollbarX is the terminal column of the scrollbar, or -1 when the
// bar is not drawn.
func (m *model) scrollbarX() int {
	if !m.ready || !m.transcript.Viewport.Scrollable() {
		return -1
	}
	x := m.promptOffset() + m.transcript.Viewport.Width() - 1
	return x
}
