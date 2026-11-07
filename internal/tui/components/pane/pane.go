// Package pane owns main-panel frames, regions and split geometry.
package pane

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

type Rect struct{ X, Y, Width, Height int }

func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

type SplitLayout struct {
	Main, Side Rect
	Covered    bool
}

// Split covers the main pane when the side pane cannot fit beside it.
func Split(area Rect, sideOpen bool, minimum int) SplitLayout {
	l := SplitLayout{Main: area}
	if !sideOpen {
		return l
	}
	l.Side = area
	if area.Width < minimum {
		l.Covered = true
		return l
	}
	l.Main.Width = area.Width / 2
	l.Side.X += l.Main.Width
	l.Side.Width -= l.Main.Width
	return l
}

type Props struct {
	Width, Height, FooterHeight int
	LeftInset, RightInset       int
	Outlined, BottomEdge, Close bool
}

type Layout struct {
	Props
	Header, Body, Footer, CloseHit Rect
	FooterDivider                  int
}

func Measure(p Props) Layout {
	p.Width, p.Height = max(p.Width, 1), max(p.Height, 1)
	if p.Outlined {
		if p.Width >= 3 {
			p.LeftInset, p.RightInset = 1, 1
		} else {
			p.Outlined = false
		}
	}
	p.LeftInset = min(max(p.LeftInset, 0), p.Width-1)
	p.RightInset = min(max(p.RightInset, 0), p.Width-p.LeftInset-1)
	inner := max(p.Width-p.LeftInset-p.RightInset, 1)
	bottom := 0
	if p.BottomEdge {
		bottom = 1
	}
	foot := max(p.FooterHeight, 1)
	body := max(p.Height-3-1-foot-bottom, 0)
	l := Layout{Props: p, Header: Rect{p.LeftInset, 1, inner, 1}, Body: Rect{p.LeftInset, 3, inner, body}}
	l.FooterDivider = l.Body.Y + body
	l.Footer = Rect{p.LeftInset, l.FooterDivider + 1, inner, foot}
	if p.Close {
		l.CloseHit = Rect{p.LeftInset + max(inner-3, 0), 1, min(3, inner), 1}
	}
	return l
}

type Styles struct{ Border, Title, Focus lipgloss.Style }

func Render(l Layout, title, body string, footer []string, styles Styles, focused bool) []string {
	border, heading := styles.Border, styles.Title
	if focused {
		border, heading = styles.Focus, styles.Focus
	}
	inner := l.Body.Width
	rows := []string{strings.Repeat("─", inner)}
	if l.Close {
		rows = append(rows, cell.Line(heading.Render(title), max(inner-3, 0))+" × ")
	} else {
		rows = append(rows, cell.Line(heading.Render(title), inner))
	}
	rows = append(rows, strings.Repeat("─", inner))
	if l.Body.Height > 0 {
		rows = append(rows, strings.Split(cell.Frame(body, inner, l.Body.Height), "\n")...)
	}
	rows = append(rows, strings.Repeat("─", inner))
	rows = append(rows, strings.Split(cell.Frame(strings.Join(footer, "\n"), inner, l.Footer.Height), "\n")...)
	if l.BottomEdge {
		rows = append(rows, strings.Repeat("─", inner))
	}
	for i, row := range rows {
		rule := i == 0 || i == 2 || i == l.FooterDivider || (l.BottomEdge && i == len(rows)-1)
		if rule && !l.Outlined {
			rows[i] = border.Render(strings.Repeat("─", l.Width))
			continue
		}
		left, right := strings.Repeat(" ", l.LeftInset), strings.Repeat(" ", l.RightInset)
		if l.Outlined {
			left, right = "│", "│"
			switch i {
			case 0:
				left, right = "┌", "┐"
			case 2, l.FooterDivider:
				left, right = "├", "┤"
			case len(rows) - 1:
				if l.BottomEdge {
					left, right = "└", "┘"
				}
			}
			left, right = border.Render(left), border.Render(right)
		}
		if rule {
			row = border.Render(row)
		}
		rows[i] = left + cell.Line(row, inner) + right
	}
	return strings.Split(cell.Frame(strings.Join(rows, "\n"), l.Width, l.Height), "\n")
}
