package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/tui/components/cell"
)

const (
	navigatorWidth    = 28
	navigatorMinWidth = 110
)

// Terminal cells, rather than byte or rune counts, own the frame geometry.
// Both live in components/cell so a form, a popup and a pane cannot drift
// in how they measure; these stay as the root package's spelling of them.
func cellLine(s string, width int) string { return cell.Line(s, width) }

func cellFrame(s string, width, height int) string { return cell.Frame(s, width, height) }
func (m *model) sidebarVisible() bool {
	return !m.sidebarHidden && m.termW >= navigatorMinWidth && m.termH >= 18 &&
		m.dialog == nil
}
func (m *model) fitColumns() {
	width := m.termW - 2
	if m.sidebarVisible() {
		width -= navigatorWidth + 3
	} else {
		if m.navFocused {
			m.input.Focus()
		}
		m.navFocused = false
	}
	m.transcript.Viewport.SetWidth(max(width, 1))
}
func (m *model) identityLine() string { return m.badgeLine() }
func (m *model) promptOffset() int {
	if m.sidebarVisible() {
		return navigatorWidth + 4
	}
	return 1
}
func (m *model) paneHeight() int    { return max(m.termH-4-m.footerRows(), 1) }
func (m *model) navigatorRows() int { return max(m.paneHeight()-1, 1) }

func (m *model) sidebarView() string {
	rows := []string{}
	visible := m.navigatorRows()
	nav := m.nav
	nav.clamp(visible)
	end := min(nav.Offset+visible, len(nav.entries))
	for i := nav.Offset; i < end; i++ {
		e := m.nav.entries[i]
		age := strings.TrimSuffix(ageString(e.updated), " ago")
		if age == "just now" {
			age = "now"
		}
		age = cellLine(age, 4)
		line := cellLine(" "+e.title, navigatorWidth-5)
		selected := (m.navFocused && i == m.nav.Cursor) || (!m.navFocused && sameSession(e.dir, m.stateFile))
		if selected {
			_, _, style := popupStyles()
			rows = append(rows, style.Render(line+" "+age))
		} else {
			rows = append(rows, line+" "+m.styles.dim.Render(age))
		}
	}
	if len(m.nav.entries) == 0 {
		label := "No saved sessions yet"
		if m.nav.loading {
			label = "Loading sessions…"
		}
		rows = append(rows, m.styles.dim.Render(label))
	}
	for len(rows) < m.navigatorRows() {
		rows = append(rows, "")
	}
	rows = append(rows, m.sidebarDetails())
	return cellFrame(strings.Join(rows, "\n"), navigatorWidth, m.paneHeight())
}

func (m *model) sidebarDetails() string {
	if len(m.nav.entries) == 0 {
		return "No session selected"
	}
	nav := m.nav
	nav.clamp(m.navigatorRows())
	e := nav.entries[nav.Cursor]
	if !m.navFocused {
		for _, entry := range nav.entries {
			if sameSession(entry.dir, m.stateFile) {
				e = entry
				break
			}
		}
	}
	noun := "messages"
	if e.msgs == 1 {
		noun = "message"
	}
	details := fmt.Sprintf("%s  %d %s", nonEmpty(e.mode), e.msgs, noun)
	if state := m.entryState(e); state != "" {
		details = state + "  " + details
	}
	return m.styles.dim.Render(cellLine(details, navigatorWidth))
}

func (m *model) entryState(e sessionEntry) string {
	if sameSession(e.dir, m.stateFile) {
		switch m.state {
		case stRunning:
			return "Working"
		case stStopping:
			return "Stopping"
		case stAsk, stPermission:
			return "Needs input"
		case stDone:
			if m.runErr != nil {
				return "Failed"
			}
		}
	}
	if e.status != "" && e.status != "Saved" {
		return e.status
	}
	return ""
}
func (m *model) navigatorKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "ctrl+q" {
		m.quit = true
		m.cancel()
		return m, tea.Quit
	}
	switch msg.String() {
	case "esc":
		m.navFocused = false
		return m, m.input.Focus()
	case "up", "down", "pgup", "pgdown":
		m.nav.Update(msg, len(m.nav.entries), m.navigatorRows())
	case "enter", "ctrl+r", "ctrl+d":
		if m.state != stDone {
			m.appendBlock(markerBlock("Stop the run before switching sessions."))
			return m, nil
		}
		if len(m.nav.entries) == 0 {
			return m, nil
		}
		dir := m.nav.entries[m.nav.Cursor].dir
		m.openSessions()
		for i, e := range m.sessions.entries {
			if e.dir == dir {
				m.sessions.Cursor = i
				break
			}
		}
		if msg.String() != "enter" {
			m.sessions.fromNavigator = true
			return m.sessionsKey(msg)
		}
		return m, m.openSelected()
	}
	return m, nil
}

// Body starts below the single global menu and per-pane status row.
const paneTop = 4
