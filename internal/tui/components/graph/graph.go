// Package graph draws and navigates a cell-based graph without domain knowledge.
package graph

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}
type Port struct {
	Key, Label string
}
type Node struct {
	ID, Title, Label, Detail string
	DetailTone               Tone
	Inputs, Outputs          []Port
	Point
}

// Tone selects a presentation style without interpreting domain state.
type Tone uint8

const (
	ToneNormal Tone = iota
	ToneSuccess
	ToneWarning
	ToneError
	ToneDisabled
)

type Edge struct {
	From, To         string
	FromPort, ToPort string
	Data             bool
}
type Model struct {
	Compact  bool
	Nodes    []Node
	Edges    []Edge
	Selected string
	Pan      Point
}

// Styles are supplied by the host so graph drawing does not choose a palette.
type Styles struct {
	Normal, Title, Focus, Success, Warning, Error, Disabled lipgloss.Style
}

const NodeWidth = 32
const NodeHeight = 8

func (m Model) nodeWidth() int {
	if m.Compact {
		return 16
	}
	return NodeWidth
}
func (n Node) Height() int { return 7 + max(1, len(n.Inputs), len(n.Outputs)) }
func (m Model) height(n Node) int {
	if m.Compact {
		return 3
	}
	return n.Height()
}
func (m Model) portRow(n Node, key string, output bool) int {
	if m.Compact {
		return 1
	}
	ports := n.Inputs
	if output {
		ports = n.Outputs
	}
	for i, p := range ports {
		if p.Key == key {
			return 4 + i
		}
	}
	return 4
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
		if x >= n.X && x < n.X+m.nodeWidth() && y >= n.Y && y < n.Y+m.height(n) {
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
		if n.Y+m.height(n) > m.Pan.Y+h {
			m.Pan.Y = max(0, n.Y+m.height(n)-h)
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
func (m Model) View(w, h int, styles Styles) string {
	normal := styles.Normal
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
	incoming, outgoing := map[[2]string]bool{}, map[[2]string]bool{}
	budget := 30000
	failed := map[[2]string]bool{}
	for _, e := range m.edges() {
		a, ok := nodes[e.From]
		b, ok2 := nodes[e.To]
		if !ok || !ok2 {
			continue
		}
		incoming[[2]string{e.To, e.ToPort}], outgoing[[2]string{e.From, e.FromPort}] = true, true
		path := m.routeSegments(a, b, e.FromPort, e.ToPort, used, &budget)
		if len(path) == 0 {
			failed[[2]string{a.ID, e.FromPort}] = true
		}
		visiblePath(path, m.Pan, w, h, func(p Point, mask int, arrow bool) {
			stroke := strokes[p]
			stroke.data = stroke.data || e.Data
			stroke.focused = stroke.focused || e.From == m.Selected || e.To == m.Selected
			stroke.mask |= mask
			stroke.arrow = stroke.arrow || arrow
			strokes[p] = stroke
		})
		for _, p := range path {
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
		box := m.card(n, styles, incoming, outgoing, failed)
		for j, s := range box {
			y := n.Y + j - m.Pan.Y
			x := n.X - m.Pan.X
			if y < 0 || y >= h || x >= w || x+m.nodeWidth() <= 0 {
				continue
			}
			start := max(x, 0)
			end := min(x+m.nodeWidth(), w)
			segment := ansi.Cut(s, start-x, end-x)
			rows[y] = ansi.Cut(rows[y], 0, start) + segment + ansi.Cut(rows[y], end, w)
		}
	}
	return strings.Join(rows, "\n")
}
