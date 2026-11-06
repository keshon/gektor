package graph

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestClippingHitAndPan(t *testing.T) {
	m := Model{Nodes: []Node{{ID: "a", Label: "日本語", Point: Point{2, 1}}, {ID: "b", Point: Point{60, 9}}}, Selected: "a", Edges: []Edge{{From: "a", To: "b", Data: true}}}
	if m.Hit(3, 2) != "a" {
		t.Fatal("hit missed")
	}
	m.SelectNext(1)
	m.Reveal(35, 12)
	if m.Pan.X == 0 {
		t.Fatal("selection not revealed")
	}
	for _, size := range []Point{{35, 12}, {5, 3}, {100, 20}} {
		v := m.View(size.X, size.Y, lipgloss.NewStyle(), lipgloss.NewStyle())
		rows := strings.Split(v, "\n")
		if len(rows) != size.Y {
			t.Fatal("height")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != size.X {
				t.Fatalf("width %d: %q", ansi.StringWidth(row), row)
			}
		}
	}
}

func TestOverviewPreservesPositions(t *testing.T) {
	m := Model{Nodes: []Node{{ID: "a", Point: Point{50, 80}}, {ID: "b", Point: Point{90, 90}}}}
	v := m.Overview(40)
	if m.Nodes[0].X != 50 || !v.Compact {
		t.Fatal("overview changed original positions")
	}
	if v.Hit(2, 1) != "a" {
		t.Fatal("compact hit failed")
	}
	for _, line := range strings.Split(v.View(40, 10, lipgloss.NewStyle(), lipgloss.NewStyle()), "\n") {
		if ansi.StringWidth(line) != 40 {
			t.Fatal(line)
		}
	}
}
