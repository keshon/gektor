package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/tui/components/popup"
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
	if m.chrome.Dialog != nil {
		if m.chrome.Dialog.Table != nil {
			if m.chrome.Dialog.Table.List {
				return m.controlHints(popup.Shortcut{Key: "↑↓", Action: "select"}, popup.Shortcut{Key: "Esc", Action: "close"})
			}
			if m.chrome.Dialog.Table.Model.Layout(m.chrome.TableProps()).Capacity == 0 {
				return m.controlHints(popup.Shortcut{Key: "Esc", Action: "close"})
			}
			if m.chrome.Dialog.Table.Model.MoreOpen {
				return m.controlHints(popup.Shortcut{Key: "Esc", Action: "back"})
			}
			return m.controlHints(popup.Shortcut{Key: "Esc", Action: "back"}, popup.Shortcut{Key: "/", Action: "filter"}, popup.Shortcut{Key: "Tab", Action: "controls"})
		}
		if m.chrome.Dialog.Form != nil {
			return m.controlHints(popup.Shortcut{Key: "Esc", Action: "cancel"}, popup.Shortcut{Key: "Tab", Action: "controls"}, popup.Shortcut{Key: "Ctrl+O", Action: "newline"})
		}
		return m.controlHints(popup.Shortcut{Key: "Esc", Action: "close"}, popup.Shortcut{Key: "↑↓", Action: "scroll"})
	}
	hint := m.actionHint()
	if m.state == stRunning && m.progress.Detail != "" && (m.progress.Phase == "waiting" || m.progress.Phase == "retry") {
		hint = m.controlHints(popup.Shortcut{Key: "Esc", Action: "stop"}) + "  " + m.progress.Detail + " · " + formatElapsed(time.Since(m.progressStarted))
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
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
