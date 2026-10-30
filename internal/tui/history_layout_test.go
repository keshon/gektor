package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/run"
	"github.com/keshon/gektor/internal/workspace"
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
	root := filepath.Join(t.TempDir(), "workspace-name")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	m.ws = ws
	m.stateFile = filepath.Join("sessions", "655e57ba", "state.json")
	m.sessionTitle = "Calculator mockup"
	line := ansi.Strip(m.menuLine())
	if !strings.HasSuffix(line, "workspace-name · 655e57ba · Calculator mockup ") {
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

func TestOutlinedChromeKeepsOuterBoundsOnResize(t *testing.T) {
	m := sizeModelTo(t, testModel(), 120, 30)
	m.minimal = false
	for _, width := range []int{40, 80, 160, 64, 120} {
		m.width(tea.WindowSizeMsg{Width: width, Height: 30})
		rows := strings.Split(ansi.Strip(m.View().Content), "\n")
		if len(rows) != 30 || rows[1] != "┌"+strings.Repeat("─", width-2)+"┐" {
			t.Fatal("outer frame moved", width, rows)
		}
		for _, y := range []int{0, 1, len(rows) - 1} {
			if lipgloss.Width(rows[y]) != width {
				t.Fatal("chrome width mismatch", width, y, rows[y])
			}
		}
		cells := m.footerCells()
		if cells[0].start != 0 || cells[len(cells)-1].end != width || m.promptOffset() != 1 || m.paneWidth() != width-2 {
			t.Fatal("outer controls and inner content share the wrong inset", width, cells)
		}
		if !strings.HasPrefix(rows[len(rows)-1], " F1 ") {
			t.Fatal("first button is inset", rows[len(rows)-1])
		}
	}
}
