package tui

import (
	"fmt"
	"strings"
	"time"
)

// statusLine describes the current input; run metrics belong in the header.
func (m *model) statusLine() string {
	if m.sessions != nil {
		return m.styles.status.Render("Search names and saved session text")
	}
	if m.dialog != nil {
		if m.dialog.form != nil {
			return m.styles.status.Render("Tab fields/buttons · Shift+Enter newline · Esc cancel")
		}
		return m.styles.status.Render("Esc close  ↑↓ scroll")
	}
	hint := m.actionHint()
	if !m.transcript.Follow && m.state == stDone {
		hint += "  Scrolled up"
	}
	return m.styles.status.Render(hint)
}

func (m *model) statusWord() string {
	switch m.state {
	case stPermission:
		return "awaiting permission"
	case stAsk:
		return "waiting"
	case stStopping:
		return "stopping"
	case stDone:
		if m.runErr != nil {
			return "failed"
		}
		return "ready"
	default:
		return "working"
	}
}

// formatElapsed renders a duration as clock time: MM:SS, H:MM:SS past
// the hour. Elapsed is per-run wall clock (reset in startRun, frozen
// on done), never idle reading time.
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Round(time.Second).Seconds())
	h, m, sec := s/3600, (s%3600)/60, s%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
}

// activityFrames animate in the header badge while a run is live.
var activityFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

func (m *model) busy() bool {
	return m.evaluation != nil || m.state == stRunning || m.state == stStopping
}

// meter renders occupied context (input plus generated output). Live estimates
// are marked with ~; a new empty conversation starts at zero.
func (m *model) meter() string {
	if m.limit <= 0 {
		return "ctx " + kTokens(m.tokens)
	}
	pct := m.tokens * 100 / m.limit
	s := fmt.Sprintf("ctx %s/%s (%d%%)", kTokens(m.tokens), kTokens(m.limit), pct)
	if m.tokensEst {
		return "~" + s
	}
	return s
}

func kTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// dividerLine splits history from the bottom zone with a dim rule.
func (m *model) dividerLine() string {
	w := max(m.transcript.Viewport.Width(), 0)
	if w < minWrapWidth {
		w = minWrapWidth
	}
	return m.styles.dim.Render(strings.Repeat("─", w))
}
