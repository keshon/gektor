package pane

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

func TestTabsKeepUnicodeLabelsAndGapsInTheirOwnHitRegions(t *testing.T) {
	labels := []string{"数据", "Résumé", "Code"}
	for _, width := range []int{9, 20, 40} {
		l := Tabs(labels, 1, width, true, TabStyles{})
		for _, row := range l.Rows {
			if cell.Width(row) != width {
				t.Fatal("tab row exceeds its area", width, row)
			}
		}
		for i, hit := range l.Hits {
			label := labels[i]
			if i == 1 {
				label = "[" + label + "]"
			}
			if ansi.Cut(l.Rows[hit.Y], hit.X, hit.X+hit.Width) != label {
				t.Fatal("hit does not cover the rendered label", width, i)
			}
			if l.Hit(hit.X, hit.Y) != i || l.Hit(hit.X+hit.Width-1, hit.Y) != i {
				t.Fatal("Unicode tab cells are not clickable", width, i)
			}
			if hit.X > 0 && l.Hit(hit.X-1, hit.Y) != -1 {
				t.Fatal("gap activates a tab")
			}
		}
		if !strings.Contains(strings.Join(l.Rows, "\n"), "[Résumé]") {
			t.Fatal("active tab lacks a non-color marker")
		}
	}
}
