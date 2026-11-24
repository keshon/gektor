package agent

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/pkg/tui/screen"
)

// composerRows is shared by rendering, viewport measurement and hit testing.
// Exceptional run details and queue state remain beside the draft.
func (m *Model) composerRows() []string {
	width := m.transcript.Viewport.Width()
	rows := []string{}
	status := m.composerStatus()
	if status != "" {
		rows = append(rows, cellLine(status, width))
	}
	if m.chrome.DialogInfo().Kind != screen.NoDialog {
		if m.modern {
			return []string{strings.Repeat(" ", width)}
		}
		return append(rows, cellLine(m.styles.Dim.Render("Draft preserved"), width))
	}
	if m.state == stPermission {
		return append(rows, strings.Split(cellFrame(m.gateBar(), width, lipgloss.Height(m.gateBar())), "\n")...)
	}
	if m.modern && m.minimal {
		rows = append([]string{strings.Repeat(" ", width)}, rows...)
	}
	if m.state != stAsk {
		if summary := m.queueSummary(); summary != "" {
			rows = append(rows, cellLine(summary, width))
		}
		if len(m.draftImages) > 0 || m.clipboardBusy {
			rows = append(rows, cellLine(m.attachmentLine(), width))
		}
	}
	input := strings.Split(m.input.View(), "\n")
	background := currentPalette().Background
	if m.modern {
		background = themeColor(colorPopupBackground)
	}
	surface := lipgloss.NewStyle().Background(lipgloss.Color(background))
	for i := range m.input.Height() {
		line := ""
		if i < len(input) {
			line = truncate(input[i], width)
		}
		padding := strings.Repeat(" ", max(width-lipgloss.Width(line), 0))
		row := line + surface.Render(padding)
		rows = append(rows, row)
	}
	if m.modern {
		rows = append(rows, strings.Repeat(" ", width), m.identityLine())
		if m.minimal {
			rows = append(rows, strings.Repeat(" ", width))
		}
		for i, row := range rows {
			rows[i] = screen.PaintSurface(cellLine(row, width), background)
		}
	}
	return rows
}

// Queue receipt can happen between UI events. Refit only when the measured
// footer changes height; ordinary clock ticks should not rebuild the inspector.
func (m *Model) syncComposerHeight() {
	if m.ready && m.transcript.Viewport.Height() != max(m.panelGeometry(m.mainWidth(), len(m.composerRows()), false, true).Body.Height, 1) {
		m.fitBottom()
	}
}

func (m *Model) composerStatus() string {
	if m.state == stRunning && m.chrome.DialogInfo().Kind == screen.NoDialog && m.sessions == nil {
		if m.progress.Detail != "" && (m.progress.Phase == "waiting" || m.progress.Phase == "retry") {
			return m.styles.Dim.Render(m.progress.Detail + screen.Separator() + formatElapsed(time.Since(m.progressStarted)))
		}
		if !m.modern {
			return ""
		}
		return ""
	}
	if m.modern && m.state == stDone && m.chrome.DialogInfo().Kind == screen.NoDialog && m.sessions == nil {
		return ""
	}
	return m.statusLine()
}
func (m *Model) composerQueueMouse(msg tea.MouseMsg) bool {
	if m.chrome.DialogInfo().Kind != screen.NoDialog || m.state == stPermission || m.state == stAsk || m.queueSummary() == "" || !isMouseClick(msg) || msg.Mouse().Button != tea.MouseLeft {
		return false
	}
	layout := m.panelGeometry(m.mainWidth(), len(m.composerRows()), false, true)
	y := 1 + layout.Footer.Y
	if m.composerStatus() != "" {
		y++
	}
	if m.modern && m.minimal {
		y++
	}
	// The count is an entry point; the preview remains plain text.
	label := strings.Split(m.queueSummary(), screen.Separator())[0]
	x := layout.Footer.X
	if m.modern {
		label = cellLine(m.queueSummary(), m.transcript.Viewport.Width())
	}
	if msg.Mouse().Y == y && msg.Mouse().X >= x && msg.Mouse().X < x+lipgloss.Width(label) {
		m.openInputQueue()
		return true
	}
	return false
}
