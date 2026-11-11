package agent

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
)

// screenContent keeps the composer inside the conversation column and the
// function-key bar outside both panes.
func (m *Model) screenContent() string {
	if !m.ready {
		return "starting..."
	}
	paneHeader := cellLine(m.identityLine(), m.transcript.Viewport.Width())

	gated := (m.state == stPermission || m.state == stAsk) && m.gatePopup.Pending()
	body := m.transcript.Viewport.View()
	body = m.suggestionView(body)
	if gated {
		body = m.gateView(body)
	} else if m.state == stPermission || m.state == stAsk {
		body = m.gateVP.View()
	}

	bodyHeight := m.transcript.Viewport.Height()
	body = cellFrame(body, m.transcript.Viewport.Width(), bodyHeight)
	footer := m.composerRows()
	layout := m.panelGeometry(m.mainWidth(), len(footer), false, true)
	focused := m.chrome.Dialog == nil && m.sessions == nil && !m.chrome.Header.Focused && (!m.inspectorVisible() || !m.inspector.focused)
	rows := pane.Render(layout, paneHeader, body, footer, m.paneStyles(), focused)
	content := "\n" + strings.Join(rows, "\n")

	content = cellFrame(content, max(m.termW, 0), max(m.termH, 0))
	if m.sessions != nil {
		content = m.overlaySearch(content)
	}
	content = m.composeInspector(content)

	return content
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.screenContent())
	// Theme only the character grid; terminal defaults and margins belong to the host.
	owner := m.activeInputOwner()
	if !m.ready || owner == focusDialog || owner == focusHeader {
		return v
	}
	if m.sessions != nil {
		g := m.searchGeometry()
		if m.sessions.mode == sessRename {
			v.Cursor = m.note.Cursor()
			if v.Cursor != nil {
				v.Cursor.X += g.x + 2
				v.Cursor.Y += g.noteRow
			}
		} else if m.sessions.mode == sessList {
			v.Cursor = m.sessions.filter.Cursor()
			if v.Cursor != nil {
				v.Cursor.X += g.x + 2
				v.Cursor.Y += g.y + 2
			}
		}
	} else if m.state == stPermission {
		if m.gstage == gsReject {
			v.Cursor = m.note.Cursor()
			if v.Cursor != nil {
				v.Cursor.X += m.promptOffset()
				v.Cursor.Y += m.termH - m.footerRows() - 1
			}
		}
	} else if owner == focusInput {
		v.Cursor = m.input.Cursor()
		if v.Cursor != nil {
			v.Cursor.X += m.promptOffset()
			v.Cursor.Y += m.termH - m.footerRows() - m.input.Height()
		}
	}
	return v
}

// Paint every cell explicitly. Terminal default colors (OSC 10/11) are not
// reliable across native cursor and input repaints in all Windows hosts.
// Restore only reset colors, preserving semantic text and popup surfaces.
func paintScreen(content string) string { return screen.Paint(content) }
