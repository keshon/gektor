package pane

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/pkg/tui/components/cell"
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

func TestFilledTabsKeepPaddingAndClickTargetsWithoutBrackets(t *testing.T) {
	labels := []string{"Properties", "Result", "Code"}
	for _, width := range []int{20, 40, 60} {
		tabs := Tabs(labels, 1, width, true, TabStyles{Filled: true})
		for _, row := range tabs.Rows {
			if cell.Width(row) != width || strings.ContainsAny(ansi.Strip(row), "[]") {
				t.Fatal("filled tabs have legacy brackets or overflow", width, row)
			}
		}
		for i, hit := range tabs.Hits {
			label := ansi.Strip(ansi.Cut(tabs.Rows[hit.Y], hit.X, hit.X+hit.Width))
			if label != " "+labels[i]+" " || tabs.Hit(hit.X, hit.Y) != i || tabs.Hit(hit.X+hit.Width-1, hit.Y) != i {
				t.Fatal("filled tab target differs from its label", width, i, label)
			}
			if hit.X > 0 && tabs.Hit(hit.X-1, hit.Y) != -1 {
				t.Fatal("tab gap is clickable")
			}
		}
	}
}
