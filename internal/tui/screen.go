package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

// screenContent keeps the composer inside the conversation column and the
// function-key bar outside both panes.
func (m *model) screenContent() string {
	if !m.ready {
		return "starting..."
	}
	if m.workflowActive() && m.sessions == nil {
		return m.workflowScreen()
	}
	// A settings section used to short-circuit here and build its own screen:
	// a menu line, a form and an action bar, framed as bare content. No
	// header, no rules, no side borders - so the one screen you open to change
	// how the app looks was the only one that did not look like the app. It
	// takes the normal path now, where a form is drawn the way every other
	// dialog is drawn: inside the pane, on the shared surface.
	paneHeader := cellLine(m.identityLine(), m.transcript.Viewport.Width())

	gated := (m.state == stPermission || m.state == stAsk) && m.gatePopup.Pending()
	body := m.transcript.Viewport.View()
	body = m.suggestionView(body)
	if gated {
		body = m.gateView(body)
	} else if m.state == stPermission || m.state == stAsk {
		body = m.gateVP.View()
	}
	if m.dialog != nil {
		body = m.overlayDialog(body)
	}
	bodyHeight := m.transcript.Viewport.Height()
	body = cellFrame(body, m.transcript.Viewport.Width(), bodyHeight)
	var bottom string
	switch {
	case m.dialog != nil:
		bottom = m.styles.dim.Render("Draft preserved")
	case m.state == stPermission:
		bottom = m.gateBar()
	default:
		bottom = m.input.View()
		if (len(m.draftImages) > 0 || m.clipboardBusy) && m.state != stAsk {
			bottom = cellLine(m.attachmentLine(), m.transcript.Viewport.Width()) + "\n" + bottom
		}
	}
	if m.hasQueued() && m.dialog == nil && m.state != stPermission && m.state != stAsk {
		bottom = cellLine(m.controlHints(popup.Shortcut{Key: "Ctrl+E", Action: "edit"}, popup.Shortcut{Key: "Ctrl+X", Action: "cancel"})+"  Queued: "+m.queuedLabel(), m.transcript.Viewport.Width()) + "\n" + bottom
	}
	if m.sess != nil && m.dialog == nil && m.state != stPermission && m.state != stAsk {
		msg, count, claimed := m.sess.InputPreview()
		if count > 0 {
			preview := strings.Join(strings.Fields(msg.Content), " ")
			if len(msg.Images) > 0 {
				preview = fmt.Sprintf("[%d images] %s", len(msg.Images), preview)
			}
			label := fmt.Sprintf("Pending %d · latest: %s", count, preview)
			hint := m.controlHints(popup.Shortcut{Key: "Ctrl+E", Action: "edit latest"}, popup.Shortcut{Key: "Ctrl+X", Action: "cancel latest"})
			if claimed > 0 {
				label = fmt.Sprintf("Accepted %d · waiting for active tools", claimed)
				if count > claimed {
					label += fmt.Sprintf(" · pending %d: %s", count-claimed, preview)
				} else {
					label += " · " + preview
					hint = m.controlHints(popup.Shortcut{Key: "Esc", Action: "stop"}) + "  Accepted input cannot be recalled"
				}
			}
			bottom = cellLine(label, m.transcript.Viewport.Width()) + "\n" + cellLine(hint, m.transcript.Viewport.Width()) + "\n" + bottom
		}
	}
	bottom = cellFrame(bottom, m.transcript.Viewport.Width(), lipgloss.Height(bottom))
	status := m.statusLine()
	status = cellFrame(status, m.transcript.Viewport.Width(), lipgloss.Height(status))
	footer := strings.Split(status+"\n"+bottom, "\n")
	layout := m.panelGeometry(m.mainWidth(), len(footer), false, true)
	focused := m.dialog == nil && m.sessions == nil && !m.header.focused && (!m.inspectorVisible() || !m.inspector.focused)
	rows := pane.Render(layout, paneHeader, body, footer, m.paneStyles(), focused)
	content := m.menuLine() + "\n" + strings.Join(rows, "\n")
	if m.actionBarVisible() {
		content += "\n" + m.actionBar()
	}

	content = cellFrame(content, max(m.termW, 0), max(m.termH, 0))
	if m.sessions != nil {
		content = m.overlaySearch(content)
	}
	content = m.composeInspector(content)
	if m.header.menu != "" {
		content = m.overlayHeader(content)
	}
	return content
}

func (m *model) View() tea.View {
	v := tea.NewView(paintScreen(m.screenContent()))
	// Theme only the character grid; terminal defaults and margins belong to the host.
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true
	owner := m.activeInputOwner()
	if !m.ready || owner == focusDialog || owner == focusHeader || m.workflowActive() {
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
func paintScreen(content string) string {
	sequence := func(code int, color string) string {
		r, g, b, _ := lipgloss.Color(color).RGBA()
		return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", code, r>>8, g>>8, b>>8)
	}
	fg := sequence(38, themeColor(colorPopupText))
	bg := sequence(48, currentPalette().background)
	reset := fg + bg
	restore := strings.NewReplacer(
		"\x1b[m", "\x1b[m"+reset,
		"\x1b[0m", "\x1b[0m"+reset,
		"\x1b[39m", fg,
		"\x1b[49m", bg,
	)
	return reset + restore.Replace(content) + "\x1b[m"
}
