package pane

import (
	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

type TabStyles struct{ Normal, Active, Focus lipgloss.Style }

type TabLayout struct {
	Rows []string
	Hits []Rect
}

// Tabs keeps each label together and returns the same cell bounds for clicks.
// Active identifies the displayed page; focused identifies its input owner.
func Tabs(labels []string, active, width int, focused bool, styles TabStyles) TabLayout {
	width = max(width, 1)
	l := TabLayout{Hits: make([]Rect, len(labels))}
	row, x, y := "", 0, 0
	for i, label := range labels {
		style := styles.Normal
		if i == active {
			label = "[" + label + "]"
			style = styles.Active
			if focused {
				style = styles.Focus
			}
		}
		w := min(cell.Width(label), width)
		if x > 0 && x+2+w > width {
			l.Rows = append(l.Rows, cell.Line(row, width))
			row, x, y = "", 0, y+1
		}
		if x > 0 {
			row += "  "
			x += 2
		}
		l.Hits[i] = Rect{X: x, Y: y, Width: w, Height: 1}
		row += style.Render(cell.Line(label, w))
		x += w
	}
	if len(labels) > 0 {
		l.Rows = append(l.Rows, cell.Line(row, width))
	}
	return l
}

func (l TabLayout) Hit(x, y int) int {
	for i, r := range l.Hits {
		if r.Contains(x, y) {
			return i
		}
	}
	return -1
}
