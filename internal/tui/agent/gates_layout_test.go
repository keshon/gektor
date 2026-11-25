package agent

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestGateHintsKeepTheCurrentStageAndFitEveryTheme(t *testing.T) {
	defer setTheme("dark")
	m := sizeModel(t, testModel())
	m.state = stPermission
	m.openGatePopup()
	for _, theme := range config.ThemeNames() {
		m.applyTheme(theme)
		for _, stage := range []gateStage{gsPermit, gsAlways, gsReject} {
			m.gstage = stage
			for _, width := range []int{20, 36, 74} {
				hint := ansi.Strip(m.gateHint(width))
				want := "ESC back"
				if stage == gsPermit {
					want = "ESC deny"
				}
				if ansi.StringWidth(hint) != width || !strings.Contains(hint, want) {
					t.Fatalf("%s stage %d width %d: missing complete cancellation hint: %q", theme, stage, width, hint)
				}
				if m.gstage != stage || !m.gatePopup.Pending() || m.gatePopup.Current().Value != "y" {
					t.Fatal("hint rendering changed approval state")
				}
			}
		}
	}
	m.gstage = gsPermit
	m.gatePreview = "proposal"
	if !strings.Contains(ansi.Strip(m.gateHint(74)), "F4 full proposal") {
		t.Fatal("proposal review shortcut is missing")
	}
}

func TestCompactApprovalShowsPromptWhileTranscriptIsScrolledUp(t *testing.T) {
	defer screen.SetModern(false)
	for _, style := range []string{"commander", "modern"} {
		for _, size := range []tea.WindowSizeMsg{{Width: 22, Height: 20}, {Width: 40, Height: 10}} {
			m := sizeModel(t, testModel())
			m.applyAppearance(config.Appearance{Style: style, Frame: "minimal"})
			for range 40 {
				m.appendBlock(userBlock("Earlier conversation"))
			}
			m.testUpdate(size)
			m.testUpdate(tea.KeyPressMsg{Code: tea.KeyPgUp})
			m.testUpdate(eventMsg(events.Event{Name: "awaiting_input", Fields: map[string]any{
				"kind": "permission", "prompt": "Review this action\n" + strings.Repeat("More details\n", 30),
			}}))
			before := m.transcript.Viewport.YOffset()
			if !strings.Contains(ansi.Strip(m.testView().Content), "Review this action") {
				t.Fatalf("%s %dx%d: approval prompt is hidden", style, size.Width, size.Height)
			}
			_, bounds := m.gateLayout()
			if bounds.Height != 0 {
				continue
			}
			m.testUpdate(tea.MouseWheelMsg{X: 4, Y: m.transcriptTop(), Button: tea.MouseWheelDown})
			if m.gateVP.YOffset() == 0 || m.transcript.Viewport.YOffset() != before {
				t.Fatalf("%s %dx%d: wheel did not scroll the visible prompt", style, size.Width, size.Height)
			}
		}
	}
}

func TestModernApprovalClickUsesTheGateConfirmationFlow(t *testing.T) {
	defer screen.SetModern(false)
	m, answered := openPermissionGate(t)
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
	rows := strings.Split(ansi.Strip(m.testView().Content), "\n")
	for y, row := range rows {
		x := strings.Index(row, "Allow for this session")
		if x < 0 {
			continue
		}
		m.testUpdate(tea.MouseClickMsg{X: ansi.StringWidth(row[:x]), Y: y, Button: tea.MouseLeft})
		if m.gstage != gsAlways || !m.hub.HasPending() {
			t.Fatal("session approval did not wait for confirmation")
		}
		m.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
		if reply := awaitReply(t, answered); reply.Answer != "a" {
			t.Fatalf("approval reply = %q", reply.Answer)
		}
		return
	}
	t.Fatal("session approval row is missing")
}
