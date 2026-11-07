package pane

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestToolbarPreservesSurfaceGapsWhenCompacting(t *testing.T) {
	actions := []ToolAction{
		{Key: "F1", Label: "Help", Enabled: true},
		{Key: "Ctrl+P", CompactKey: "^P", Label: "Search", Enabled: true},
		{Key: "Ctrl+Q", CompactKey: "^Q", Label: "Quit", Enabled: true},
	}
	styles := ToolbarStyles{
		Key:   lipgloss.NewStyle().Background(lipgloss.Color("#385b76")),
		Label: lipgloss.NewStyle().Background(lipgloss.Color("#385b76")),
		Gap:   lipgloss.NewStyle().Background(lipgloss.Color("#10161c")),
	}
	for width := 3; width <= 120; width++ {
		layout := Toolbar(width, actions, styles)
		for i := 1; i < len(layout.Cells); i++ {
			previous := layout.Cells[i-1].Bounds
			gap := previous.X + previous.Width
			if layout.Cells[i].Bounds.X-gap != 1 {
				t.Fatalf("width %d: actions lost their one-column gap", width)
			}
			if got := ansi.Cut(layout.Row, gap, gap+1); ansi.Strip(got) != " " || !strings.Contains(got, styles.Gap.Render(" ")) {
				t.Fatalf("width %d: gap does not use the surrounding surface: %q", width, got)
			}
			if layout.Hit(gap, 0) != -1 {
				t.Fatalf("width %d: gap activates an action", width)
			}
		}
	}
}

func TestToolbarFitsOneRowAndRejectsDisabledActionsAndGaps(t *testing.T) {
	actions := []ToolAction{
		{Key: "F1", Label: "帮助", CompactLabel: "Help", Enabled: true},
		{Key: "Ctrl+S", CompactKey: "^S", Label: "Save", Enabled: false},
		{Key: "Esc", Label: "Back", Enabled: true},
	}
	for _, width := range []int{0, 1, 4, 40, 81, 120} {
		l := Toolbar(width, actions, ToolbarStyles{})
		if ansi.StringWidth(l.Row) != width || strings.Contains(l.Row, "\n") {
			t.Fatal("toolbar is not exactly one bounded row", width, l.Row)
		}
		for i, item := range l.Cells {
			if item.Bounds.X > 0 && l.Hit(item.Bounds.X-1, 0) != -1 {
				t.Fatal("gap activates a command")
			}
			for _, x := range []int{item.Bounds.X, item.Bounds.X + item.Bounds.Width - 1} {
				want := i
				if !actions[i].Enabled {
					want = -1
				}
				if l.Hit(x, 0) != want || l.Hit(x, 1) != -1 {
					t.Fatal("toolbar hit ignores availability or row bounds", width, i)
				}
			}
		}
	}
}

func TestToolbarCompactsControlKeysAndLabels(t *testing.T) {
	actions := []ToolAction{{Key: "Ctrl+P", CompactKey: "^P", Label: "Search sessions", CompactLabel: "Find", Enabled: true}}
	if row := Toolbar(10, actions, ToolbarStyles{}).Row; row != " ^P Find  " {
		t.Fatalf("compact action is unreadable: %q", row)
	}
	if row := Toolbar(24, actions, ToolbarStyles{}).Row; !strings.Contains(row, " CTRL+P Search sessions") {
		t.Fatal("roomy toolbar shortened its action", row)
	}
}
