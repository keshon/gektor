package graph

import (
	"container/heap"
	"sort"
)

// Search card boundaries and port lanes, not every empty world cell. Panning
// does not change the route; the renderer clips segments before drawing cells.
func (m Model) routeSegments(from, to Node, fromPort, toPort string, used map[Point]bool, budget *int) []Point {
	start := Point{from.X + m.nodeWidth(), from.Y + m.portRow(from, fromPort, true)}
	end := Point{to.X - 1, to.Y + m.portRow(to, toPort, false)}
	clear := func(a, b Point) bool {
		for _, n := range m.Nodes {
			right, bottom := n.X+m.nodeWidth()-1, n.Y+m.height(n)-1
			if a.Y == b.Y && a.Y >= n.Y && a.Y <= bottom && max(a.X, b.X) >= n.X && min(a.X, b.X) <= right {
				return false
			}
			if a.X == b.X && a.X >= n.X && a.X <= right && max(a.Y, b.Y) >= n.Y && min(a.Y, b.Y) <= bottom {
				return false
			}
		}
		return true
	}
	if !clear(start, start) || !clear(end, end) {
		return nil
	}
	xset, yset := map[int]bool{start.X: true, end.X: true}, map[int]bool{start.Y: true, end.Y: true}
	for _, n := range m.Nodes {
		xset[n.X-1], xset[n.X+m.nodeWidth()] = true, true
		yset[n.Y-1], yset[n.Y+m.height(n)] = true, true
	}
	coordinates := func(set map[int]bool) ([]int, map[int]int) {
		values := make([]int, 0, len(set))
		for value := range set {
			values = append(values, value)
		}
		sort.Ints(values)
		indices := map[int]int{}
		for i, value := range values {
			indices[value] = i
		}
		return values, indices
	}
	xs, xi := coordinates(xset)
	ys, yi := coordinates(yset)
	distance := func(a, b Point) int { return abs(a.X-b.X) + abs(a.Y-b.Y) }
	queue := routeQueue{{point: start, score: 10 * distance(start, end)}}
	heap.Init(&queue)
	cost := map[Point]int{start: 0}
	parent := map[Point]Point{}
	for visited := 0; len(queue) > 0 && visited < 12000; visited++ {
		if *budget <= 0 {
			return nil
		}
		*budget--
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
			x, y := xi[p.X]+delta.X, yi[p.Y]+delta.Y
			if x < 0 || x >= len(xs) || y < 0 || y >= len(ys) {
				continue
			}
			next := Point{xs[x], ys[y]}
			if !clear(p, next) {
				continue
			}
			step := 10 * distance(p, next)
			for cell := range used {
				if cell != start && cell != end && cell.X >= min(p.X, next.X) && cell.X <= max(p.X, next.X) && cell.Y >= min(p.Y, next.Y) && cell.Y <= max(p.Y, next.Y) {
					step += 20
				}
			}
			if prev, ok := parent[p]; ok && direction(prev, p) != direction(p, next) {
				step += 3
			}
			candidate := cost[p] + step
			if old, ok := cost[next]; ok && old <= candidate {
				continue
			}
			cost[next], parent[next] = candidate, p
			heap.Push(&queue, routeItem{point: next, cost: candidate, score: candidate + 10*distance(next, end)})
		}
	}
	return nil
}

func visiblePath(path []Point, pan Point, width, height int, draw func(Point, int, bool)) {
	if len(path) == 1 {
		p := path[0]
		if p.X >= pan.X && p.X < pan.X+width && p.Y >= pan.Y && p.Y < pan.Y+height {
			draw(p, west|east, true)
		}
	}
	for i := 0; i+1 < len(path); i++ {
		a, b := path[i], path[i+1]
		loX, hiX := max(min(a.X, b.X), pan.X), min(max(a.X, b.X), pan.X+width-1)
		loY, hiY := max(min(a.Y, b.Y), pan.Y), min(max(a.Y, b.Y), pan.Y+height-1)
		for y := loY; y <= hiY; y++ {
			for x := loX; x <= hiX; x++ {
				p, mask, arrow := (Point{x, y}), 0, false
				if p != a {
					mask |= direction(p, a)
				} else if i == 0 {
					mask |= west
				} else {
					mask |= direction(p, path[i-1])
				}
				if p != b {
					mask |= direction(p, b)
				} else if i+1 == len(path)-1 {
					mask |= east
					arrow = true
				} else {
					mask |= direction(p, path[i+2])
				}
				draw(p, mask, arrow)
			}
		}
	}
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
	byPair := map[[4]string]Edge{}
	for _, e := range m.Edges {
		key := [4]string{e.From, e.To, e.FromPort, e.ToPort}
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
		if out[i].To != out[j].To {
			return out[i].To < out[j].To
		}
		if out[i].FromPort != out[j].FromPort {
			return out[i].FromPort < out[j].FromPort
		}
		return out[i].ToPort < out[j].ToPort
	})
	return out
}

func (m Model) PortAt(x, y int) (string, bool) {
	id, _, output := m.PortHit(x, y)
	return id, output
}
func (m Model) PortHit(x, y int) (string, string, bool) {
	x += m.Pan.X
	y += m.Pan.Y
	for i := len(m.Nodes) - 1; i >= 0; i-- {
		n := m.Nodes[i]
		output := x == n.X+m.nodeWidth()-1
		if x != n.X && !output {
			continue
		}
		ports := n.Inputs
		if output {
			ports = n.Outputs
		}
		if ports == nil {
			ports = []Port{{}}
		}
		for _, p := range ports {
			if y == n.Y+m.portRow(n, p.Key, output) {
				return n.ID, p.Key, output
			}
		}
	}
	return "", "", false
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
