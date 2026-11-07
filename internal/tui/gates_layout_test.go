package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/config"
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
