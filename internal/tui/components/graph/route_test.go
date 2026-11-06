package graph

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestRoutesAvoidCardsAndRerouteAfterMoving(t *testing.T) {
	m := Model{Nodes: []Node{{ID: "a", Point: Point{2, 5}}, {ID: "b", Point: Point{75, 5}}, {ID: "obstacle", Point: Point{35, 4}}}}
	check := func() {
		budget := 30000
		path := m.route(m.Nodes[0], m.Nodes[1], nil, &budget)
		if len(path) == 0 {
			t.Fatal("route missing")
		}
		for i, p := range path {
			for _, n := range m.Nodes {
				if p.X >= n.X && p.X < n.X+NodeWidth && p.Y >= n.Y && p.Y < n.Y+NodeHeight {
					t.Fatal("route crossed a card", p)
				}
			}
			if i > 0 && abs(p.X-path[i-1].X)+abs(p.Y-path[i-1].Y) != 1 {
				t.Fatal("disconnected route")
			}
		}
	}
	check()
	m.Nodes[1].Point = Point{0, 17}
	check()
	m.Edges = []Edge{{From: "a", To: "b"}, {From: "a", To: "b", Data: true}}
	if edges := m.edges(); len(edges) != 1 || !edges[0].Data {
		t.Fatal("duplicate control/data route", edges)
	}
	v := m.View(110, 28, lipgloss.NewStyle(), lipgloss.NewStyle())
	if !strings.Contains(v, "●") || !strings.ContainsAny(v, "┌┐└┘") {
		t.Fatal(v)
	}
	budget := 30000
	path := m.route(m.Nodes[0], m.Nodes[1], nil, &budget)
	rows := strings.Split(ansi.Strip(v), "\n")
	corner := false
	for i := 1; i < len(path)-1; i++ {
		p := path[i]
		if direction(p, path[i-1])|direction(p, path[i+1]) == east|west || direction(p, path[i-1])|direction(p, path[i+1]) == north|south {
			continue
		}
		if p.X < 0 || p.X >= 110 || p.Y < 0 || p.Y >= 28 {
			continue
		}
		if !strings.ContainsRune("┌┐└┘├┤┬┴╪", []rune(rows[p.Y])[p.X]) {
			t.Fatal("disconnected corner", p, rows[p.Y])
		}
		corner = true
	}
	if !corner {
		t.Fatal("detoured route had no visible corners")
	}
	m.Pan = Point{3, 2}
	id, output := m.PortAt(m.Nodes[0].X+NodeWidth-1-3, m.Nodes[0].Y+NodeHeight/2-2)
	if id != "a" || !output {
		t.Fatal("panned port hit failed")
	}
}

func TestOverlappingCardsShowUnroutableMarker(t *testing.T) {
	m := Model{Nodes: []Node{{ID: "a", Point: Point{2, 2}}, {ID: "b", Point: Point{2 + NodeWidth, 2}}}, Edges: []Edge{{From: "a", To: "b", Data: true}}}
	// The adjacent card covers the source's exit cell. It cannot be routed
	// honestly; the renderer must not draw a fake line through either card.
	budget := 10
	if path := m.route(m.Nodes[0], m.Nodes[1], nil, &budget); len(path) != 0 {
		t.Fatal(path)
	}
	if v := m.View(60, 12, lipgloss.NewStyle(), lipgloss.NewStyle()); !strings.Contains(v, "!") {
		t.Fatal("unroutable connection hidden", v)
	}
}
