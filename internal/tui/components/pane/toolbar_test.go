package pane

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

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
	if row := Toolbar(24, actions, ToolbarStyles{}).Row; !strings.Contains(row, " Ctrl+P Search sessions") {
		t.Fatal("roomy toolbar shortened its action", row)
	}
}
