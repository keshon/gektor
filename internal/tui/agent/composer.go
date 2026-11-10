package agent

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// composerRows is shared by rendering, viewport measurement and hit testing.
// Routine shortcuts live in the toolbar; exceptional run details remain here.
func (m *Model) composerRows() []string {
	width := m.transcript.Viewport.Width()
	rows := []string{}
	status := m.composerStatus()
	if status != "" {
		rows = append(rows, cellLine(status, width))
	}
	if m.dialog != nil {
		return append(rows, cellLine(m.styles.dim.Render("Draft preserved"), width))
	}
	if m.state == stPermission {
		return append(rows, strings.Split(cellFrame(m.gateBar(), width, lipgloss.Height(m.gateBar())), "\n")...)
	}
	if m.state != stAsk {
		if summary := m.queueSummary(); summary != "" {
			rows = append(rows, cellLine(summary, width))
		}
		if len(m.draftImages) > 0 || m.clipboardBusy {
			rows = append(rows, cellLine(m.attachmentLine(), width))
		}
	}
	return append(rows, strings.Split(cellFrame(m.input.View(), width, m.input.Height()), "\n")...)
}

// Queue receipt can happen between UI events. Refit only when the measured
// footer changes height; ordinary clock ticks should not rebuild the inspector.
func (m *Model) syncComposerHeight() {
	if m.ready && !m.workflowActive() && m.transcript.Viewport.Height() != max(m.panelGeometry(m.mainWidth(), len(m.composerRows()), false, true).Body.Height, 1) {
		m.fitBottom()
	}
}

func (m *Model) composerStatus() string {
	if m.state == stRunning && m.dialog == nil && m.sessions == nil && m.actionBarVisible() {
		if m.progress.Detail != "" && (m.progress.Phase == "waiting" || m.progress.Phase == "retry") {
			return m.styles.dim.Render(m.progress.Detail + " · " + formatElapsed(time.Since(m.progressStarted)))
		}
		return ""
	}
	return m.statusLine()
}
func (m *Model) composerQueueMouse(msg tea.MouseMsg) bool {
	if m.dialog != nil || m.state == stPermission || m.state == stAsk || m.queueSummary() == "" || !isMouseClick(msg) || msg.Mouse().Button != tea.MouseLeft {
		return false
	}
	layout := m.panelGeometry(m.mainWidth(), len(m.composerRows()), false, true)
	y := 1 + layout.Footer.Y
	if m.composerStatus() != "" {
		y++
	}
	// The count is an entry point; the preview remains plain text.
	label := strings.Split(m.queueSummary(), " · ")[0]
	x := layout.Footer.X
	if msg.Mouse().Y == y && msg.Mouse().X >= x && msg.Mouse().X < x+lipgloss.Width(label) {
		m.openInputQueue()
		return true
	}
	return false
}
