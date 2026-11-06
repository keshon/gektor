// Package graph draws and navigates a cell-based graph without domain knowledge.
package graph

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/cell"
	"strings"
)

type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}
type Node struct {
	ID, Label, Detail string
	Point
}
type Edge struct {
	From, To string
	Data     bool
}
type Model struct {
	Compact  bool
	Nodes    []Node
	Edges    []Edge
	Selected string
	Pan      Point
}

const NodeWidth = 24
const NodeHeight = 5

func (m Model) nodeWidth() int {
	if m.Compact {
		return 16
	}
	return NodeWidth
}
func (m Model) nodeHeight() int {
	if m.Compact {
		return 3
	}
	return NodeHeight
}

// Overview returns a compact, temporary layout. Original positions are untouched.
func (m Model) Overview(width int) Model {
	m.Nodes = append([]Node(nil), m.Nodes...)
	m.Compact = true
	m.Pan = Point{}
	cols := max(1, (width-2)/18)
	for i := range m.Nodes {
		m.Nodes[i].Point = Point{1 + (i%cols)*18, (i / cols) * 4}
	}
	return m
}

func (m *Model) Hit(x, y int) string {
	x += m.Pan.X
	y += m.Pan.Y
	for i := len(m.Nodes) - 1; i >= 0; i-- {
		n := m.Nodes[i]
		if x >= n.X && x < n.X+m.nodeWidth() && y >= n.Y && y < n.Y+m.nodeHeight() {
			return n.ID
		}
	}
	return ""
}
func (m *Model) SelectNext(delta int) {
	if len(m.Nodes) == 0 {
		return
	}
	i := 0
	for j, n := range m.Nodes {
		if n.ID == m.Selected {
			i = j
			break
		}
	}
	m.Selected = m.Nodes[(i+delta+len(m.Nodes))%len(m.Nodes)].ID
}
func (m *Model) Reveal(w, h int) {
	for _, n := range m.Nodes {
		if n.ID != m.Selected {
			continue
		}
		if n.X < m.Pan.X {
			m.Pan.X = n.X
		}
		if n.Y < m.Pan.Y {
			m.Pan.Y = n.Y
		}
		if n.X+m.nodeWidth() > m.Pan.X+w {
			m.Pan.X = max(0, n.X+m.nodeWidth()-w)
		}
		if n.Y+m.nodeHeight() > m.Pan.Y+h {
			m.Pan.Y = max(0, n.Y+m.nodeHeight()-h)
		}
	}
}
func (m *Model) Move(dx, dy int) {
	for i := range m.Nodes {
		if m.Nodes[i].ID == m.Selected {
			m.Nodes[i].X = max(0, m.Nodes[i].X+dx)
			m.Nodes[i].Y = max(0, m.Nodes[i].Y+dy)
		}
	}
}
func (m Model) View(w, h int, normal, selected lipgloss.Style) string {
	w = max(1, w)
	h = max(1, h)
	grid := make([][]rune, h)
	for y := range grid {
		grid[y] = []rune(strings.Repeat(" ", w))
	}
	type stroke struct {
		mask          int
		data, focused bool
		arrow         bool
	}
	strokes := map[Point]stroke{}
	used := map[Point]bool{}
	nodes := map[string]Node{}
	for _, n := range m.Nodes {
		nodes[n.ID] = n
	}
	incoming, outgoing := map[string]bool{}, map[string]bool{}
	budget := 30000
	failed := map[string]bool{}
	for _, e := range m.edges() {
		a, ok := nodes[e.From]
		b, ok2 := nodes[e.To]
		if !ok || !ok2 {
			continue
		}
		incoming[e.To], outgoing[e.From] = true, true
		path := m.route(a, b, used, &budget)
		if len(path) == 0 {
			failed[a.ID] = true
		}
		for i, p := range path {
			stroke := strokes[p]
			stroke.data = stroke.data || e.Data
			stroke.focused = stroke.focused || e.From == m.Selected || e.To == m.Selected
			if i == 0 {
				stroke.mask |= west
			} else {
				stroke.mask |= direction(p, path[i-1])
			}
			if i == len(path)-1 {
				stroke.mask |= east
				stroke.arrow = true
			} else {
				stroke.mask |= direction(p, path[i+1])
			}
			strokes[p] = stroke
			used[p] = true
		}
	}
	for p, stroke := range strokes {
		x, y := p.X-m.Pan.X, p.Y-m.Pan.Y
		if x >= 0 && x < w && y >= 0 && y < h {
			r := lineRune(stroke.mask, stroke.data)
			if stroke.arrow {
				r = '▶'
			}
			grid[y][x] = r
		}
	}
	rows := make([]string, h)
	for y := range rows {
		var row strings.Builder
		kindAt := func(x int) int {
			stroke, ok := strokes[Point{x + m.Pan.X, y + m.Pan.Y}]
			if !ok {
				return 0
			}
			if stroke.focused {
				return 2
			}
			return 1
		}
		for start := 0; start < w; {
			kind := kindAt(start)
			end := start + 1
			for end < w && kindAt(end) == kind {
				end++
			}
			style := normal
			if kind == 1 {
				style = normal.Faint(true)
			} else if kind == 2 {
				style = normal.Bold(true)
			}
			row.WriteString(style.Render(string(grid[y][start:end])))
			start = end
		}
		rows[y] = row.String()
	}
	for _, n := range m.Nodes {
		box := []string{"┌" + strings.Repeat("─", m.nodeWidth()-2) + "┐", "│" + cell.Line(ansi.Strip(n.ID), m.nodeWidth()-2) + "│", "│" + cell.Line(ansi.Strip(n.Label), m.nodeWidth()-2) + "│", "│" + cell.Line(ansi.Strip(n.Detail), m.nodeWidth()-2) + "│", "└" + strings.Repeat("─", m.nodeWidth()-2) + "┘"}
		if m.Compact {
			box = []string{box[0], box[1], box[len(box)-1]}
		}
		portRow := m.nodeHeight() / 2
		inPort, outPort := '○', '○'
		if incoming[n.ID] {
			inPort = '●'
		}
		if outgoing[n.ID] {
			outPort = '●'
		}
		if failed[n.ID] {
			outPort = '!'
		}
		port := []rune(box[portRow])
		port[0] = inPort
		port[len(port)-1] = outPort
		box[portRow] = string(port)
		style := normal
		if n.ID == m.Selected {
			style = selected
		}
		for j, s := range box {
			y := n.Y + j - m.Pan.Y
			x := n.X - m.Pan.X
			if y < 0 || y >= h || x >= w || x+m.nodeWidth() <= 0 {
				continue
			}
			start := max(x, 0)
			end := min(x+m.nodeWidth(), w)
			segment := ansi.Cut(s, start-x, end-x)
			rows[y] = ansi.Cut(rows[y], 0, start) + style.Render(segment) + ansi.Cut(rows[y], end, w)
		}
	}
	return strings.Join(rows, "\n")
}
