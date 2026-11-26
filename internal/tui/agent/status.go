package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/pkg/tui/components/popup"
	"github.com/keshon/gektor/pkg/tui/screen"
)

// controlHints shares the whole-group shortcut renderer with modal footers.
func (m *Model) controlHints(shortcuts ...popup.Shortcut) string {
	return strings.TrimRight(popup.Shortcuts(m.transcript.Viewport.Width(), m.shortcutStyles(), shortcuts...), " ")
}

// statusLine describes the current input; run metrics belong in the header.
func (m *Model) statusLine() string {
	if m.sessions != nil {
		return m.styles.Status.Render("Search names and saved session text")
	}
	if m.chrome.DialogInfo().Kind != screen.NoDialog {
		return m.controlHints(m.chrome.DialogShortcuts()...)
	}

	hint := m.actionHint()
	if m.state == stRunning && m.progress.Detail != "" && (m.progress.Phase == "waiting" || m.progress.Phase == "retry") {
		hint = m.controlHints(popup.Shortcut{Key: "Esc", Action: "stop"}) + "  " + m.progress.Detail + screen.Separator() + formatElapsed(time.Since(m.progressStarted))
	}
	if m.state == stStopping {
		hint += "  Stopping… draft preserved"
	}
	if selected := m.inspectorHint(); selected != "" {
		hint = selected
	}
	if !m.transcript.Follow && m.state == stDone {
		hint += "  Scrolled up"
	}
	return m.styles.Status.Render(hint)
}

func (m *Model) statusWord() string {
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

func (m *Model) busy() bool {
	return m.evaluation != nil || m.state == stRunning || m.state == stStopping
}

// meter renders occupied context (input plus generated output). Live estimates
// are marked with ~; a new empty conversation starts at zero.
func (m *Model) meter() string {
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
	if n >= 1000000 {
		return fmt.Sprintf("%.2fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func (m *Model) statusAnimation() string {
	palette := currentPalette()
	if palette.StatusAnimation != "" {
		return palette.StatusAnimation
	}
	return config.DefaultStatusAnimation(palette.Modern)
}

func (m *Model) activityLabel(phase string) string {
	switch m.statusAnimation() {
	case "text":
		return phase
	case "square":
		return "▪ " + phase
	default:
		return string(activityFrames[m.spinnerFrame%len(activityFrames)]) + " " + phase
	}
}

func (m *Model) paintActivity(phase string) string {
	// Each half of the spinner cycle holds for half a second.
	if (m.state != stRunning && m.evaluation == nil) || m.spinnerFrame < len(activityFrames)/2 {
		return phase
	}
	switch m.statusAnimation() {
	case "square":
		return " " + strings.TrimPrefix(phase, "▪")
	case "text":
		return strings.Repeat(" ", len(phase))
	default:
		return phase
	}
}
