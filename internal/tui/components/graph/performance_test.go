package graph

import (
	"fmt"
	"testing"

	"charm.land/lipgloss/v2"
)

func BenchmarkDistantNodeDrag(b *testing.B) {
	for _, distance := range []int{100, 2000, 10000} {
		b.Run(fmt.Sprint(distance), func(b *testing.B) {
			m := Model{Nodes: []Node{{ID: "a", Point: Point{2, 1}}, {ID: "b", Point: Point{distance, 9}}}, Edges: []Edge{{From: "a", To: "b", Data: true}}, Selected: "b"}
			style := lipgloss.NewStyle()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m.Nodes[1].Y = 9 + i%2
				m.Pan = Point{distance - 60, i % 3}
				m.View(100, 28, style, style)
			}
		})
	}
}

func TestDistantRoutesKeepBoundedSearchAndClipBeforeDrawing(t *testing.T) {
	for _, distance := range []int{100, 10000, 1000000} {
		m := Model{Nodes: []Node{{ID: "a", Point: Point{2, 1}}, {ID: "b", Point: Point{distance, 9}}}}
		budget := 200
		path := m.routeSegments(m.Nodes[0], m.Nodes[1], "", "", nil, &budget)
		if len(path) == 0 || budget <= 0 {
			t.Fatal("distance exhausted the routing budget", distance)
		}
		cells := 0
		visiblePath(path, Point{distance - 60, 0}, 100, 28, func(p Point, mask int, arrow bool) {
			cells++
			if p.X < distance-60 || p.X >= distance+40 || p.Y < 0 || p.Y >= 28 {
				t.Fatal("offscreen cell was expanded", p)
			}
		})
		if cells == 0 || cells > 256 {
			t.Fatal("drawing cost grew with world distance", distance, cells)
		}
	}
}
