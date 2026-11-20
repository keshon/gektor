package popup

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestMenuClampsAndMapsScrolledRowsToCommands(t *testing.T) {
	items := []MenuItem{
		{Label: "启用", Shortcut: "Enter", Checked: true, Enabled: true},
		{Separator: true},
		{Label: "Disabled"},
		{Label: "Status: ready", Info: true},
		{Label: "Last command", Enabled: true},
	}
	normal := lipgloss.NewStyle().Background(lipgloss.Color("#111111"))
	styles := MenuStyles{Surface: Styles{Normal: normal, Border: normal, Selected: normal}, Disabled: normal}
	for _, size := range [][2]int{{2, 2}, {40, 20}, {80, 24}, {120, 40}, {160, 50}} {
		p := MenuProps{X: size[0] - 5, Y: 1, ScreenWidth: size[0], ScreenHeight: size[1], MinWidth: 24, MaxWidth: 58, MaxRows: 3, Selected: 4}
		l := Menu(items, p, styles)
		if size[0] < 4 {
			if len(l.Rows) != 0 || l.Hit(0, 0) != -1 {
				t.Fatal("menu escaped a tiny screen")
			}
			continue
		}
		if l.X < 0 || l.X+l.Width > size[0] || l.Y+len(l.Rows) > size[1] || l.Offset != 2 {
			t.Fatal("menu bounds or scroll window incorrect", size, l)
		}
		for _, row := range l.Rows {
			if ansi.StringWidth(row) != l.Width {
				t.Fatal("menu row exceeds its frame", row)
			}
		}
		for _, point := range [][2]int{{l.X, l.Y + 3}, {l.X + l.Width - 1, l.Y + 3}, {l.X + 2, l.Y}, {l.X + 2, l.Y + 1}, {l.X + 2, l.Y + 2}} {
			if l.Hit(point[0], point[1]) != -1 {
				t.Fatal("frame or unavailable row activated a command", point)
			}
		}
		if l.Hit(l.X+2, l.Y+3) != 4 || !strings.Contains(ansi.Strip(l.Rows[3]), "Last command") {
			t.Fatal("visible command does not match its hit")
		}
	}
}

func TestMenuNavigationSkipsInformationSeparatorsAndDisabledRows(t *testing.T) {
	items := []MenuItem{{Label: "First", Enabled: true}, {Separator: true}, {Label: "Disabled"}, {Label: "Info", Info: true}, {Label: "Last", Enabled: true}}
	for _, test := range []struct{ start, delta, want int }{{1, 1, 4}, {3, -1, 0}, {5, 1, 0}, {-1, -1, 4}} {
		if got := MenuNext(items, test.start, test.delta); got != test.want {
			t.Fatal("menu navigation stopped at an unavailable row", test, got)
		}
	}
}

func TestMenuUppercasesShortcutLabelsWithoutChangingBindings(t *testing.T) {
	items := []MenuItem{{Label: "Delete", Shortcut: "delete", Enabled: true}, {Label: "Paste JSON", Shortcut: "p", Enabled: true}, {Label: "Save", Shortcut: "Ctrl+s", Enabled: true}}
	l := Menu(items, MenuProps{ScreenWidth: 40, ScreenHeight: 10}, MenuStyles{})
	text := ansi.Strip(strings.Join(l.Rows, "\n"))
	for _, want := range []string{"Delete", "DELETE", "Paste JSON", " P", "CTRL+S"} {
		if !strings.Contains(text, want) {
			t.Fatal("menu lost key casing or action label", text)
		}
	}
	if items[0].Shortcut != "delete" || items[1].Shortcut != "p" || l.Hit(l.X+2, l.Y+1) != 0 {
		t.Fatal("display formatting changed source bindings or hit targets")
	}
}
