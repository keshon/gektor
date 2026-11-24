package agent

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestModernInspectorUsesFullHeightAndDistinctSurface(t *testing.T) {
	defer screen.SetModern(false)
	defer setTheme("dark")
	for _, width := range []int{80, 120} {
		for _, frame := range []string{"minimal", "outlined"} {
			m := inspectorModel(t, width)
			m.applyAppearance(config.Appearance{Style: "modern", Frame: frame})
			m.applyTheme("dark")
			m.openInspector(len(m.transcript.blocks) - 1)
			m.inspector.pane.Viewport.GotoTop()
			area := m.screenPanes(true).Side
			layout := m.panelGeometry(area.Width, 1, true, false)
			view := ansi.Strip(m.testView().Content)
			rows := strings.Split(view, "\n")
			if layout.Footer.Height != 0 || strings.Contains(view, "[focused]") {
				t.Fatal("Modern inspector retained Commander chrome", width, frame)
			}
			row := rows[area.Y+layout.Body.Y]
			at := strings.Index(row, "Arguments")
			if at < 0 || ansi.StringWidth(row[:at]) != area.X+layout.Body.X+1 {
				t.Fatal("inspector body did not shift one cell left", width, frame, row)
			}
			if frame == "minimal" {
				surface := m.inspectionRows("Inspect", "", m.inspector.pane.View(), area.Width, true, true)
				if surface[0] == ansi.Strip(surface[0]) || themeColor(colorPopupBackground) == currentPalette().Background {
					t.Fatal("borderless inspector has no separate surface")
				}
			}
			parent := m.transcript.Viewport.YOffset()
			x := area.X + layout.Body.X + layout.Body.Width - 1
			y := area.Y + layout.Body.Y
			m.handleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			m.handleMouse(tea.MouseMotionMsg{X: x, Y: m.termH + 4, Button: tea.MouseLeft})
			m.handleMouse(tea.MouseReleaseMsg{X: x, Y: m.termH + 4})
			if !m.inspector.pane.Viewport.AtBottom() || m.transcript.Viewport.YOffset() != parent {
				t.Fatal("footer removal broke inspector scrolling", width, frame)
			}
			m.handleMouse(tea.MouseClickMsg{X: area.X + layout.CloseHit.X, Y: area.Y + layout.CloseHit.Y, Button: tea.MouseLeft})
			if m.inspectorVisible() {
				t.Fatal("inspector close target drifted", width, frame)
			}
		}
	}
}

func TestInspectorFocusUsesColorInsteadOfLabelInBothStyles(t *testing.T) {
	defer screen.SetModern(false)
	for _, style := range []string{"commander", "modern"} {
		m := inspectorModel(t, 120)
		m.applyAppearance(config.Appearance{Style: style, Frame: "outlined"})
		m.openInspector(len(m.transcript.blocks) - 1)
		if !m.inspector.focused || strings.Contains(ansi.Strip(m.testView().Content), "[focused]") {
			t.Fatal("inspector focus label remains", style)
		}
	}
}
