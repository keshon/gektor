package graph

import (
	"container/heap"
	"sort"
)

// Routes use world cells, so panning clips the same path instead of changing it.
// Search is bounded to keep a dense or overlapping canvas responsive.
func (m Model) route(from, to Node, used map[Point]bool, budget *int) []Point {
	start := Point{from.X + m.nodeWidth(), from.Y + m.nodeHeight()/2}
	end := Point{to.X - 1, to.Y + m.nodeHeight()/2}
	blocked := func(p Point) bool {
		for _, n := range m.Nodes {
			if p.X >= n.X && p.X < n.X+m.nodeWidth() && p.Y >= n.Y && p.Y < n.Y+m.nodeHeight() {
				return true
			}
		}
		return false
	}
	if blocked(start) || blocked(end) {
		return nil
	}
	minX, maxX, minY, maxY := min(start.X, end.X)-4, max(start.X, end.X)+4, min(start.Y, end.Y)-4, max(start.Y, end.Y)+4
	for _, n := range m.Nodes {
		minX = min(minX, n.X-3)
		maxX = max(maxX, n.X+m.nodeWidth()+3)
		minY = min(minY, n.Y-3)
		maxY = max(maxY, n.Y+m.nodeHeight()+3)
	}
	distance := func(a, b Point) int { return abs(a.X-b.X) + abs(a.Y-b.Y) }
	queue := routeQueue{{point: start, score: distance(start, end)}}
	heap.Init(&queue)
	cost := map[Point]int{start: 0}
	parent := map[Point]Point{}
	for visited := 0; len(queue) > 0 && visited < 12000; visited++ {
		if *budget <= 0 {
			return nil
		}
		*budget -= 1
		item := heap.Pop(&queue).(routeItem)
		p := item.point
		if item.cost != cost[p] {
			continue
		}
		if p == end {
			path := []Point{p}
			for p != start {
				p = parent[p]
				path = append(path, p)
			}
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path
		}
		for _, delta := range []Point{{1, 0}, {0, 1}, {0, -1}, {-1, 0}} {
			next := Point{p.X + delta.X, p.Y + delta.Y}
			if next.X < minX || next.X > maxX || next.Y < minY || next.Y > maxY || blocked(next) {
				continue
			}
			step := 10
			if used[next] && next != end && next != start {
				step += 20
			}
			if prev, ok := parent[p]; ok && (p.X-prev.X != delta.X || p.Y-prev.Y != delta.Y) {
				step += 3
			}
			candidate := cost[p] + step
			if old, ok := cost[next]; ok && old <= candidate {
				continue
			}
			cost[next] = candidate
			parent[next] = p
			heap.Push(&queue, routeItem{point: next, cost: candidate, score: candidate + 10*distance(next, end)})
		}
	}
	return nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

type routeItem struct {
	point       Point
	cost, score int
}
type routeQueue []routeItem

func (q routeQueue) Len() int           { return len(q) }
func (q routeQueue) Less(i, j int) bool { return q[i].score < q[j].score }
func (q routeQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *routeQueue) Push(v any)        { *q = append(*q, v.(routeItem)) }
func (q *routeQueue) Pop() any          { old := *q; v := old[len(old)-1]; *q = old[:len(old)-1]; return v }

func (m Model) edges() []Edge {
	byPair := map[[2]string]Edge{}
	for _, e := range m.Edges {
		key := [2]string{e.From, e.To}
		previous := byPair[key]
		e.Data = e.Data || previous.Data
		byPair[key] = e
	}
	out := make([]Edge, 0, len(byPair))
	for _, e := range byPair {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}

func (m Model) PortAt(x, y int) (string, bool) {
	x += m.Pan.X
	y += m.Pan.Y
	for i := len(m.Nodes) - 1; i >= 0; i-- {
		n := m.Nodes[i]
		if y != n.Y+m.nodeHeight()/2 {
			continue
		}
		if x == n.X {
			return n.ID, false
		}
		if x == n.X+m.nodeWidth()-1 {
			return n.ID, true
		}
	}
	return "", false
}

const (
	north = 1
	east  = 2
	south = 4
	west  = 8
)

func direction(a, b Point) int {
	if b.X > a.X {
		return east
	}
	if b.X < a.X {
		return west
	}
	if b.Y > a.Y {
		return south
	}
	return north
}
func lineRune(mask int, data bool) rune {
	switch mask {
	case east, west, east | west:
		if data {
			return '─'
		}
		return '┄'
	case north, south, north | south:
		if data {
			return '│'
		}
		return '┆'
	case east | south:
		return '┌'
	case west | south:
		return '┐'
	case north | east:
		return '└'
	case north | west:
		return '┘'
	case north | east | south:
		return '├'
	case north | west | south:
		return '┤'
	case east | west | south:
		return '┬'
	case east | west | north:
		return '┴'
	default:
		return '╪'
	}
}
