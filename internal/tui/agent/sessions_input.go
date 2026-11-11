package agent

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

// sessionsKey routes browser input. Rename owns typing until Enter or Escape;
// confirmation modes accept only their explicit choices.
func (m *Model) sessionsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	defer m.fitBottom()

	s := m.sessions
	if msg.String() == "ctrl+c" || msg.String() == "ctrl+q" {
		m.quit = true
		m.cancel()
		return m, tea.Quit
	}

	if s.mode == sessRename {
		switch msg.String() {
		case "enter":
			m.commitRename()
			return m, nil
		case "esc":
			s.rename.Blur()
			s.rename.SetValue("")
			s.mode = sessList

			return m, nil
		}

		var cmd tea.Cmd
		s.rename, cmd = s.rename.Update(msg)

		return m, cmd
	}

	if s.mode == sessConfirm || s.mode == sessReassign {
		switch msg.String() {
		case "y", "Y":
			if s.mode == sessReassign {
				m.reassignSelected()
			} else {
				m.deleteSelected()
			}

			return m, nil
		case "n", "N", "esc":
			s.mode = sessList
			s.reassign = nil
			s.flash = ""

			return m, nil
		}

		return m, nil
	}

	key := msg.String()

	switch key {
	case "esc", "ctrl+p":
		m.closeSessions()
		return m, nil
	case "enter":
		return m, m.openSelected()
	case "up", "down", "pgup", "pgdown":
		s.flash = ""
		if len(s.entries) == 0 {
			return m, nil
		}

		// Selection owns cursor and viewport bounds for keys and mouse.
		s.Update(msg, len(s.entries), m.sessVisible())

		return m, nil
	case "ctrl+f":
		s.currentOnly = !s.currentOnly
		s.applyFilter()
		return m, nil
	case "ctrl+w":
		if m.state != stDone {
			s.flash = "stop the run first"
			return m, nil
		}

		if len(s.entries) == 0 {
			return m, nil
		}

		s.flash = ""
		cur := s.entries[s.Cursor]
		s.reassign = &sessionAssignment{
			directory: cur.dir,
			workspace: m.wsRoot,
			previous:  cur.workspace,
		}
		s.mode = sessReassign

		return m, nil
	case "ctrl+r":
		if len(s.entries) == 0 {
			return m, nil
		}

		s.flash = ""
		s.mode = sessRename
		s.rename = newSessionRenameInput()

		return m, s.rename.Focus()
	case "ctrl+d":
		if len(s.entries) == 0 {
			return m, nil
		}

		s.flash = ""
		s.mode = sessConfirm

		return m, nil
	}

	if m.activeInputOwner() == focusSearch {
		return m.updateSearchInput(msg)
	}

	return m, nil
}

func (m *Model) browserMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.actionBarVisible() && msg.Mouse().Y == m.termH-1 && msg.Mouse().Button == tea.MouseLeft && isMouseClick(msg) {
		return m.toolbarClick(msg.Mouse().X)
	}

	s := m.sessions
	g := m.searchGeometry()
	mouse := msg.Mouse()
	if isMouseClick(msg) && mouse.Button == tea.MouseLeft && mouse.Y == g.y+1 && mouse.X >= g.x+g.width-5 && mouse.X < g.x+g.width-2 {
		m.closeSessions()
		return m, nil
	}

	if s.mode != sessList {
		return m, nil
	}

	if m.sessionsScrollbarMouse(msg) {
		return m, nil
	}

	if mouse.X < g.x || mouse.X >= g.x+g.width || mouse.Y < g.y || mouse.Y >= g.y+g.height {
		return m, nil
	}

	switch mouse.Button {
	case tea.MouseWheelUp:
		s.Cursor--
	case tea.MouseWheelDown:
		s.Cursor++
	case tea.MouseLeft:
		if !isMouseClick(msg) {
			return m, nil
		}

		at := mouse.Y - g.y - 4
		row := at / 4
		if at < 0 || at%4 == 3 || row >= m.sessVisible() || s.Offset+row >= len(s.entries) {
			return m, nil
		}

		s.Cursor = s.Offset + row
	default:
		return m, nil
	}

	m.clampOffset()

	return m, nil
}

func (m *Model) updateSearchInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	s := m.sessions
	before := s.filter.Value()
	var cmd tea.Cmd
	s.filter, cmd = s.filter.Update(msg)
	if s.filter.Value() != before {
		s.applyFilter()
	}

	return m, cmd
}

func newSearchInput(width int) textinput.Model {
	filter := textinput.New()
	filter.SetStyles(textInputStyles(filter.Styles()))
	filter.Prompt = "> "
	filter.SetVirtualCursor(false)
	style := filter.Styles()
	style.Cursor.Shape = tea.CursorUnderline
	filter.SetStyles(style)
	filter.Placeholder = "Session name or history text"
	filter.CharLimit = 80
	filter.SetWidth(width)

	return filter
}

// The list scrolls in whole entries; the track is measured in terminal rows.
func (m *Model) sessionsScrollbarMouse(msg tea.MouseMsg) bool {
	s := m.sessions
	visible := m.sessVisible()
	if len(s.entries) <= visible {
		s.drag = scrollDrag{}
		return false
	}

	g := m.searchGeometry()
	height, total := visible*4-1, len(s.entries)*4-1
	mouse := msg.Mouse()
	row := mouse.Y - g.y - 4

	if s.drag.Active {
		switch msg.(type) {
		case tea.MouseMotionMsg, tea.MouseReleaseMsg:
			offset := (popup.ScrollOffset(row, height, total, s.drag.Grab) + 2) / 4
			s.scrollTo(offset, visible)
			if _, ok := msg.(tea.MouseReleaseMsg); ok {
				s.drag = scrollDrag{}
			}

			return true
		}
	}

	if !isMouseClick(msg) || mouse.Button != tea.MouseLeft || mouse.X != g.x+g.width-3 || row < 0 || row >= height {
		return false
	}

	top, size := popup.ScrollThumb(height, total, s.Offset*4)
	if row >= top && row < top+size {
		s.drag = scrollDrag{Active: true, Grab: row - top}
	} else if row < top {
		s.scrollTo(s.Offset-visible, visible)
	} else {
		s.scrollTo(s.Offset+visible, visible)
	}

	return true
}

func newSessionRenameInput() textinput.Model {
	input := newSearchInput(0)
	input.Prompt = "name> "
	input.Placeholder = ""

	return input
}
