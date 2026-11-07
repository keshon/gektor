package popup

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

type Shortcut struct{ Key, Action string }
type ShortcutStyles struct{ Key, Action lipgloss.Style }

// KeyLabel formats display text only; event bindings keep their original values.
func KeyLabel(key string) string { return strings.ToUpper(strings.ReplaceAll(key, "\n", " ")) }

// Shortcuts renders one hint row. Callers list essential groups first;
// groups that cannot fit are omitted whole, never shortened into partial keys.
// Hints describe keyboard actions and do not introduce mouse controls.
func Shortcuts(width int, styles ShortcutStyles, shortcuts ...Shortcut) string {
	width = max(width, 0)
	var groups []string
	used := 0
	for _, shortcut := range shortcuts {
		key, action := KeyLabel(shortcut.Key), strings.ReplaceAll(shortcut.Action, "\n", " ")
		if key == "" || action == "" {
			continue
		}
		n := cell.Width(key) + 1 + cell.Width(action)
		gap := 0
		if len(groups) > 0 {
			gap = 2
		}
		if used+gap+n > width {
			continue
		}
		groups = append(groups, styles.Key.Render(key)+" "+styles.Action.Render(action))
		used += gap + n
	}
	return cell.Line(strings.Join(groups, "  "), width)
}
