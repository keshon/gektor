package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"
)

// screenContent keeps the composer inside the conversation column and the
// function-key bar outside both panes.
func (m *model) screenContent() string {
	if !m.ready {
		return "starting..."
	}
	// A settings section used to short-circuit here and build its own screen:
	// a menu line, a form and an action bar, framed as bare content. No
	// header, no rules, no side borders - so the one screen you open to change
	// how the app looks was the only one that did not look like the app. It
	// takes the normal path now, where a form is drawn the way every other
	// dialog is drawn: inside the pane, on the shared surface.
	paneHeader := cellLine(m.identityLine(), m.transcript.Viewport.Width())

	popup := m.suggestions.Pending() && m.transcript.Viewport.Height() >= 4 && m.transcript.Viewport.Width() >= 24
	gated := (m.state == stPermission || m.state == stAsk) && m.gatePopup.Pending()
	body := m.transcript.Viewport.View()
	gap := "\n"
	if popup || gated {
		// Both overlays are spliced over the transcript's last rows, and
		// both need one row more than the pane hands them. The row comes
		// from the gap that would otherwise sit above the divider.
		body += "\n"
		gap = ""
	}
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
	if popup || gated {
		bodyHeight++
	}
	body = cellFrame(body, m.transcript.Viewport.Width(), bodyHeight)
	var bottom string
	switch {
	case m.dialog != nil:
		bottom = m.styles.dim.Render("Draft preserved")
	case m.state == stPermission:
		bottom = m.gateBar()
	default:
		bottom = m.input.View()
		if m.state == stDone && m.runErr != nil {
			bottom = m.styles.err.Render("error: "+truncate(m.runErr.Error(), 240)) + "\n" + bottom
		}
	}
	if m.queued != "" && m.dialog == nil && m.state != stPermission && m.state != stAsk {
		bottom = cellLine("Queued  Ctrl+E edit  Ctrl+X cancel: "+m.queued, m.transcript.Viewport.Width()) + "\n" + bottom
	}
	if count := m.pendingInputCount(); count > 0 && m.dialog == nil && m.state != stPermission && m.state != stAsk {
		bottom = cellLine(fmt.Sprintf("Sent: %d · waiting for the agent", count), m.transcript.Viewport.Width()) + "\n" + bottom
	}
	bottom = cellFrame(bottom, m.transcript.Viewport.Width(), lipgloss.Height(bottom))
	conversation := body + gap + "\n" + m.dividerLine() + "\n" + cellLine(m.statusLine(), m.transcript.Viewport.Width()) + "\n" + bottom
	conversation = cellFrame(conversation, m.transcript.Viewport.Width(), m.paneHeight())

	innerWidth := max(m.termW-2, 0)
	rule := strings.Repeat("─", innerWidth)
	content := m.menuLine() + "\n" + m.styles.dim.Render(rule) + "\n" + paneHeader +
		"\n" + m.styles.dim.Render(rule) + "\n" + conversation
	if m.actionBarVisible() {
		content += "\n" + m.styles.dim.Render(rule) + "\n" + m.actionBar()
	}
	if m.termW >= 2 && m.actionBarVisible() {
		rows := strings.Split(content, "\n")
		composerRule := paneTop + bodyHeight
		if gap != "" {
			composerRule++
		}
		for i := 1; i < len(rows)-1; i++ {
			left, right := "│", "│"
			if i == 1 {
				left, right = "┌", "┐"
			}
			if i == 3 {
				left, right = "├", "┤"
			}
			if i == composerRule {
				left, right = "├", "┤"
			}
			if i == len(rows)-2 {
				left, right = "└", "┘"
			}
			rows[i] = m.styles.dim.Render(left) + cellLine(rows[i], innerWidth) + m.styles.dim.Render(right)
		}
		content = strings.Join(rows, "\n")
	}
	content = cellFrame(content, max(m.termW, 0), max(m.termH, 0))
	if m.sessions != nil {
		content = m.overlaySearch(content)
	}
	if m.header.menu != "" {
		content = m.overlayHeader(content)
	}
	return content
}

func (m *model) View() tea.View {
	v := tea.NewView(paintScreen(m.screenContent()))
	// Renderer defaults color unstyled cells without overwriting popup surfaces.
	v.BackgroundColor = lipgloss.Color(currentPalette().background)
	v.ForegroundColor = lipgloss.Color(themeColor(colorPopupText))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true
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
