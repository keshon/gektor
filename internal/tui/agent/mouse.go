package agent

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/transcript"
	"github.com/keshon/gektor/internal/tui/screen"
)

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.suggestions.Dragging() && m.suggestionMouse(msg) {
		return m, nil
	}
	if m.sessions != nil {
		return m.browserMouse(msg)
	}
	if m.chrome.Dialog != nil {
		if m.actionBarVisible() && msg.Mouse().Button == tea.MouseLeft && isMouseClick(msg) && msg.Mouse().Y == m.termH-1 {
			return m.toolbarClick(msg.Mouse().X)
		}
		return m, m.chrome.DialogMouse(msg)
	}
	if m.inspectorVisible() && m.chrome.Header.Menu == "" && m.inspector.drag.Active && m.inspectorScrollbarMouse(msg) {
		return m, nil
	}
	// A grabbed thumb owns motion and release until the button is released,
	// including over the footer and outside the scrollbar column.
	if m.scrollDrag.Active && m.chrome.Dialog == nil && m.sessions == nil && m.chrome.Header.Menu == "" && m.scrollbarMouse(msg) {
		return m, nil
	}
	if handled, cmd := m.headerMouse(msg); handled {
		return m, cmd
	}
	if m.inspectorVisible() && (m.termW < inspectorSplitWidth || msg.Mouse().X >= m.mainWidth()) && msg.Mouse().Y >= 1 && msg.Mouse().Y < m.termH-m.actionBarRows() {
		if handled, cmd := m.inspectorMouse(msg); handled {
			return m, cmd
		}
	}
	if m.actionBarVisible() && msg.Mouse().Y == m.termH-1 && msg.Mouse().Button == tea.MouseLeft && isMouseClick(msg) {
		return m.toolbarClick(msg.Mouse().X)
	}

	if m.composerQueueMouse(msg) {
		return m, nil
	}
	if m.suggestionMouse(msg) {
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

func (m *Model) dialogScrollbarMouse(msg tea.MouseMsg) bool {
	return m.chrome.DialogScrollbarMouse(msg)
}

// scrollDrag is a scrollbar thumb grab: which row of the thumb the
// pointer holds, and whether it has moved since the grab.
type scrollDrag = screen.Drag

// scrollbarMouse handles press, motion and release on the transcript
// scrollbar. A grab on the thumb drags it; a click on the track pages.
// It reports whether it consumed the message.
func (m *Model) scrollbarMouse(msg tea.MouseMsg) bool {
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
		if !drag.Active {
			return false
		}
		row := min(max(mouse.Y-paneY, 0), v.Height()-1)
		drag.Moved = true
		v.SetYOffset(v.DragOffset(row, v.Height(), drag.Grab))
		*follow = v.AtBottom()
		return true
	case tea.MouseReleaseMsg:
		if !drag.Active {
			return false
		}
		previous := *drag
		*drag = scrollDrag{}
		if !previous.Moved && mouse.Button == tea.MouseLeft {
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
	*drag = scrollDrag{Active: true, Grab: max(row-top, 0)}
	return true
}

// scrollbarX is the terminal column of the scrollbar, or -1 when the
// bar is not drawn.
func (m *Model) scrollbarX() int {
	if !m.ready || !m.transcript.Viewport.Scrollable() {
		return -1
	}
	x := m.promptOffset() + m.transcript.Viewport.Width() - 1
	return x
}
