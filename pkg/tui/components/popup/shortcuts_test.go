package popup

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestShortcutsKeepWholeGroupsAndEssentialCloseHint(t *testing.T) {
	styles := ShortcutStyles{Key: lipgloss.NewStyle().Bold(true)}
	shortcuts := []Shortcut{{"Esc", "cancel"}, {"Ctrl+S", "apply"}, {"Tab", "fields"}}
	for _, width := range []int{0, 4, 10, 20, 24, 40} {
		row := Shortcuts(width, styles, shortcuts...)
		plain := ansi.Strip(row)
		if ansi.StringWidth(row) != width || strings.Contains(plain, "…") {
			t.Fatal("shortcut row overflowed or truncated a group", width, plain)
		}
		if width >= 10 && !strings.HasPrefix(plain, "ESC cancel") {
			t.Fatal("essential cancel hint disappeared", width, plain)
		}
		for _, group := range strings.Split(strings.TrimSpace(plain), "  ") {
			if group != "" && group != "ESC cancel" && group != "CTRL+S apply" && group != "TAB fields" {
				t.Fatal("partial shortcut", width, group)
			}
		}
	}
}

func TestShortcutWidthsUseTerminalCells(t *testing.T) {
	row := Shortcuts(12, ShortcutStyles{}, Shortcut{"↑↓", "选择"}, Shortcut{"E", "edit"})
	if row != "↑↓ 选择     " {
		t.Fatalf("Unicode group did not use terminal cells: %q", row)
	}
}
