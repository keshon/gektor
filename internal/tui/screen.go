package tui

import (
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
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.Workspace != nil {
		content := m.menuLine() + "\n" + m.formView()
		if m.actionBarVisible() {
			content += "\n" + m.actionBar()
		}
		return cellFrame(content, m.termW, m.termH)
	}
	paneHeader := cellLine(m.identityLine(), m.transcript.Viewport.Width())
	if m.sidebarVisible() {
		titleStyle := m.styles.dim
		if m.navFocused {
			_, _, titleStyle = popupStyles()
		}
		paneHeader = cellLine(titleStyle.Render(" SESSIONS "), navigatorWidth) + m.styles.dim.Render(" │ ") + paneHeader
	}
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
		body = m.dialogView()
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
	bottom = cellFrame(bottom, m.transcript.Viewport.Width(), lipgloss.Height(bottom))
	conversation := body + gap + "\n" + m.dividerLine() + "\n" + cellLine(m.statusLine(), m.transcript.Viewport.Width()) + "\n" + bottom
	conversation = cellFrame(conversation, m.transcript.Viewport.Width(), m.paneHeight())
	if m.sidebarVisible() {
		divider := strings.Repeat(m.styles.dim.Render(" │ ")+"\n", m.paneHeight()-1) + m.styles.dim.Render(" │ ")
		// Connect only the conversation composer rule to the vertical divider.
		rows := strings.Split(divider, "\n")
		ruleRow := bodyHeight
		if !popup {
			ruleRow++
		}
		rows[ruleRow] = m.styles.dim.Render(" ├─")
		divider = strings.Join(rows, "\n")
		conversation = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebarView(), divider, conversation)
	}
	innerWidth := max(m.termW-2, 0)
	// A full-width rule, optionally split by a junction where the sidebar
	// divider meets it. Three rules use the same expression and differ only
	// in that one rune; column is 0 when there is no sidebar.
	column := 0
	if m.sidebarVisible() {
		column = navigatorWidth + 1
	}
	rule := func(junction string) string {
		if column <= 0 {
			return strings.Repeat("─", innerWidth)
		}
		return strings.Repeat("─", column) + junction +
			strings.Repeat("─", max(innerWidth-column-1, 0))
	}
	content := m.menuLine() + "\n" + m.styles.dim.Render(rule("┬")) + "\n" + paneHeader +
		"\n" + m.styles.dim.Render(rule("┼")) + "\n" + conversation
	if m.actionBarVisible() {
		barRule := m.dividerLine()
		if column > 0 {
			barRule = m.styles.dim.Render(rule("┴"))
		}
		content += "\n" + barRule + "\n" + m.actionBar()
	}
	if m.termW >= 2 && m.actionBarVisible() {
		rows := strings.Split(content, "\n")
		for i := 1; i < len(rows)-1; i++ {
			left, right := "│", "│"
			if i == 1 {
				left, right = "┌", "┐"
			}
			if i == 3 {
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
	v := tea.NewView(m.screenContent())
	// Renderer defaults color unstyled cells without overwriting popup surfaces.
	v.BackgroundColor = lipgloss.Color(currentPalette().background)
	v.ForegroundColor = lipgloss.Color(themeColor(colorPopupText))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	owner := m.activeInputOwner()
	if !m.ready || owner == focusDialog || owner == focusHeader {
		return v
	}
	if m.sessions != nil {
		g := m.searchGeometry()
		if m.sessions.mode == sessRename {
			v.Cursor = m.note.Cursor()
			if v.Cursor != nil {
				v.Cursor.X += g.x + 1
				v.Cursor.Y += g.noteRow
			}
		} else if m.sessions.mode == sessList {
			v.Cursor = m.sessions.filter.Cursor()
			if v.Cursor != nil {
				v.Cursor.X += g.x + 1
				v.Cursor.Y += g.y + 1
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
