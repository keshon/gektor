package graph

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNamedPortsHaveDistinctRoutesAndHitTargets(t *testing.T) {
	a := Node{ID: "prepare", Label: "JavaScript", Point: Point{2, 1}, Inputs: []Port{}, Outputs: []Port{{Key: "/items", Label: "items"}, {Key: "/label", Label: "label"}}}
	b := Node{ID: "report", Label: "JavaScript", Point: Point{55, 1}, Inputs: []Port{{Key: "args/input/items", Label: "input.items"}, {Key: "args/input/label", Label: "input.label"}}, Outputs: []Port{{Label: "result"}}}
	m := Model{Nodes: []Node{a, b}, Selected: "prepare", Edges: []Edge{{From: "prepare", To: "report", FromPort: "/items", ToPort: "args/input/items", Data: true}, {From: "prepare", To: "report", FromPort: "/label", ToPort: "args/input/label", Data: true}}}
	if len(m.edges()) != 2 {
		t.Fatal("distinct field mappings collapsed")
	}
	used := map[Point]bool{}
	budget := 30000
	for i, e := range m.Edges {
		path := m.routeEdge(a, b, e.FromPort, e.ToPort, used, &budget)
		if len(path) == 0 || path[0] != (Point{a.X + NodeWidth, a.Y + 4 + i}) || path[len(path)-1] != (Point{b.X - 1, b.Y + 4 + i}) {
			t.Fatal("wrong field endpoints", path)
		}
	}
	m.Pan = Point{1, 1}
	id, key, output := m.PortHit(a.X+NodeWidth-2, a.Y+4)
	if id != "prepare" || key != "/label" || !output {
		t.Fatal("wrong named port hit", id, key, output)
	}
	view := ansi.Strip(m.View(90, 15, lipgloss.NewStyle(), lipgloss.NewStyle()))
	for _, text := range []string{"prepare", "JavaScript", "input.items", "input.label", "Not run"} {
		if !strings.Contains(view, text) {
			t.Fatal("missing card content", text, view)
		}
	}
	for _, row := range strings.Split(view, "\n") {
		if ansi.StringWidth(row) != 90 {
			t.Fatal("bad cell width", row)
		}
	}
}

func TestCardHeightAndHitIncludeAllPortRows(t *testing.T) {
	n := Node{ID: "many", Point: Point{2, 1}, Inputs: []Port{{Key: "a"}, {Key: "b"}, {Key: "c"}, {Key: "d"}}, Outputs: []Port{{}}}
	m := Model{Nodes: []Node{n}}
	if n.Height() != 11 || m.Hit(3, n.Y+n.Height()-1) != "many" || m.Hit(3, n.Y+n.Height()) != "" {
		t.Fatal("card geometry disagrees with hit test")
	}
	rows := m.card(n, lipgloss.NewStyle(), lipgloss.NewStyle(), nil, nil, nil)
	if len(rows) != n.Height() {
		t.Fatal("card footer clipped", len(rows), n.Height())
	}
}
