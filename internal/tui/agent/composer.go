package agent

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/pkg/tui/components/pane"
	"github.com/keshon/gektor/pkg/tui/screen"
)

type composerLayout struct {
	panel    pane.Layout
	rows     []string
	badges   []screen.Badge
	badgeRow int
	input    pane.Rect
	note     pane.Rect
	queue    pane.Rect
}

// composerLayout places footer content in display order and measures its
// terminal coordinates once for rendering, native carets and pointer input.
func (m *Model) composerLayout() composerLayout {
	width := m.transcript.Viewport.Width()
	palette := currentPalette()
	layout := composerLayout{badges: m.headerBadges()}
	blank := strings.Repeat(" ", width)
	status := m.composerStatus()
	if status != "" {
		layout.rows = append(layout.rows, cellLine(status, width))
	}

	switch {
	case m.chrome.DialogInfo().Kind != screen.NoDialog:
		if palette.Modern {
			layout.rows = []string{blank}
		} else {
			layout.rows = append(layout.rows, cellLine(m.styles.Dim.Render("Draft preserved"), width))
		}
	case m.state == stPermission:
		bar := m.gateBar()
		layout.rows = append(layout.rows, strings.Split(cellFrame(bar, width, lipgloss.Height(bar)), "\n")...)
		if m.gstage == gsReject {
			layout.note = pane.Rect{Y: len(layout.rows) - 1, Width: width, Height: 1}
		}
	default:
		m.composeDraft(&layout, palette, blank, width)
	}

	layout.panel = m.panelGeometry(m.mainWidth(), len(layout.rows), false, true)
	x, y := layout.panel.Footer.X, 1+layout.panel.Footer.Y
	for _, region := range []*pane.Rect{&layout.input, &layout.note, &layout.queue} {
		if region.Height != 0 {
			region.X += x
			region.Y += y
		}
	}
	if layout.badgeRow != 0 {
		layout.badgeRow += y
	}
	return layout
}

func (m *Model) composeDraft(layout *composerLayout, palette *screen.Palette, blank string, width int) {
	if palette.Modern && !palette.Outlined {
		layout.rows = append([]string{blank}, layout.rows...)
	}
	if m.state != stAsk {
		if summary := m.queueSummary(); summary != "" {
			label, _, _ := strings.Cut(summary, screen.Separator())
			if palette.Modern {
				label = summary
			}
			layout.queue = pane.Rect{Y: len(layout.rows), Width: min(lipgloss.Width(label), width), Height: 1}
			layout.rows = append(layout.rows, cellLine(summary, width))
		}
		if len(m.draftImages) > 0 || m.clipboardBusy {
			layout.rows = append(layout.rows, cellLine(m.attachmentLine(), width))
		}
	}

	background := palette.Background
	if palette.Modern {
		background = palette.Color(colorPopupBackground)
	}
	surface := lipgloss.NewStyle().Background(lipgloss.Color(background))
	input := strings.Split(m.input.View(), "\n")
	layout.input = pane.Rect{Y: len(layout.rows), Width: width, Height: m.input.Height()}
	for i := range layout.input.Height {
		line := ""
		if i < len(input) {
			line = truncate(input[i], width)
		}
		padding := strings.Repeat(" ", max(width-lipgloss.Width(line), 0))
		layout.rows = append(layout.rows, line+surface.Render(padding))
	}
	if !palette.Modern {
		return
	}

	layout.rows = append(layout.rows, blank)
	layout.badgeRow = len(layout.rows)
	layout.rows = append(layout.rows, m.badgeLine(layout.badges))
	if !palette.Outlined {
		layout.rows = append(layout.rows, blank)
	}
	for i, row := range layout.rows {
		layout.rows[i] = palette.PaintSurface(cellLine(row, width), background)
	}
}

// Queue receipt can happen between UI events. Refit only when the measured
// footer changes height; ordinary clock ticks should not rebuild the inspector.
func (m *Model) syncComposerHeight() {
	if m.ready && m.transcript.Viewport.Height() != max(m.composerLayout().panel.Body.Height, 1) {
		m.fitBottom()
	}
}

func (m *Model) composerStatus() string {
	if m.state == stRunning && m.chrome.DialogInfo().Kind == screen.NoDialog && m.sessions == nil {
		if m.progress.Detail != "" && (m.progress.Phase == "waiting" || m.progress.Phase == "retry") {
			return m.styles.Dim.Render(m.progress.Detail + screen.Separator() + formatElapsed(time.Since(m.progressStarted)))
		}
		return ""
	}
	if currentPalette().Modern && m.state == stDone && m.transcript.Follow && m.chrome.DialogInfo().Kind == screen.NoDialog && m.sessions == nil {
		return ""
	}
	return m.statusLine()
}
func (m *Model) composerQueueMouse(msg tea.MouseMsg) bool {
	mouse := msg.Mouse()
	if !isMouseClick(msg) || mouse.Button != tea.MouseLeft {
		return false
	}
	layout := m.composerLayout()
	if !layout.queue.Contains(mouse.X, mouse.Y) {
		return false
	}
	m.openInputQueue()
	return true
}
