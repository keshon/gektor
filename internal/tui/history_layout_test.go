package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/run"
)

func TestSendingMessageRestoresHistoryFollow(t *testing.T) {
	m := scrollModel(t, 120, 30)
	m.state = stRunning
	m.sess = &run.Session{}
	m.transcript.Follow = false
	m.transcript.Viewport.GotoTop()
	m.input.SetValue("Continue with these requirements")
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.transcript.Follow || !m.transcript.Viewport.AtBottom() {
		t.Fatal("sending a follow-up left the history scrolled up")
	}
	// A delivered user message also resumes follow after reopening a session.
	m.transcript.Follow = false
	m.transcript.Viewport.GotoTop()
	m.appendBlock(userBlock("New instruction"))
	if !m.transcript.Follow || !m.transcript.Viewport.AtBottom() || !strings.Contains(ansi.Strip(m.transcript.Viewport.View()), "New instruction") {
		t.Fatal("new user message is not visible")
	}
}

func TestTaskModalsKeepHistoryBehindTheirFrame(t *testing.T) {
	for _, kind := range []string{"info", "rename", "delete", "settings"} {
		t.Run(kind, func(t *testing.T) {
			m := settingsModel(t)
			m.width(tea.WindowSizeMsg{Width: 140, Height: 40})
			m.state = stDone
			m.appendBlock(answerBlock(strings.Repeat("VISIBLE HISTORY\n", 80)))
			m.transcript.Follow = false
			m.transcript.Viewport.SetYOffset(3)
			before := m.transcript.Viewport.YOffset()
			switch kind {
			case "info":
				m.openDialog("About", []string{"GEKTOR"})
			case "settings":
				m.editSettings("generation")
			default:
				m.openForm(kind+" session", "Confirm the change.", "Apply", nil, func(map[string]string) error { return nil })
			}
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "VISIBLE HISTORY") || !strings.Contains(view, "╭") {
				t.Fatal("modal cleared the document behind it")
			}
			m.closeDialog()
			if m.transcript.Viewport.YOffset() != before {
				t.Fatal("closing modal moved the reading position")
			}
		})
	}
}

func TestSessionIdentityIsRightAlignedWithoutOverlappingMenus(t *testing.T) {
	m := sizeModelTo(t, testModel(), 160, 30)
	m.stateFile = filepath.Join("sessions", "655e57ba", "state.json")
	m.sessionTitle = "Calculator mockup"
	line := ansi.Strip(m.menuLine())
	if !strings.HasSuffix(line, "655e57ba · Calculator mockup ") {
		t.Fatalf("missing right-aligned identity: %q", line)
	}
	for _, width := range []int{40, 80, 120, 160} {
		m.width(tea.WindowSizeMsg{Width: width, Height: 30})
		if lipgloss.Width(m.menuLine()) != width || !strings.Contains(ansi.Strip(m.menuLine()), "Session") {
			t.Fatal("session identity changed menu geometry")
		}
	}
}

func TestComposerRuleMeetsBothFrameEdges(t *testing.T) {
	m := sizeModelTo(t, testModel(), 120, 30)
	m.state = stDone
	for _, draft := range []string{"", "first\nsecond\nthird"} {
		m.input.SetValue(draft)
		m.fitBottom()
		rows := strings.Split(ansi.Strip(m.View().Content), "\n")
		y := paneTop + m.transcript.Viewport.Height()
		if rows[y] != "├"+strings.Repeat("─", m.termW-2)+"┤" {
			t.Fatalf("composer divider is disconnected: %q", rows[y])
		}
	}
}
