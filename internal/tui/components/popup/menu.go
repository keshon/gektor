package popup

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

type MenuItem struct {
	Label, Shortcut                   string
	Enabled, Checked, Separator, Info bool
}

type MenuProps struct {
	X, Y, ScreenWidth, ScreenHeight int
	MinWidth, MaxWidth, MaxRows     int
	Selected                        int
}

type MenuStyles struct {
	Surface  Styles
	Disabled lipgloss.Style
}

type MenuLayout struct {
	X, Y, Width, Capacity, Offset int
	Rows                          []string
	items                         []MenuItem
}

// Menu owns the attached surface and the visible source-row mapping.
func Menu(items []MenuItem, p MenuProps, styles MenuStyles) MenuLayout {
	l := MenuLayout{items: items}
	if p.ScreenWidth < 4 || p.ScreenHeight < 3 {
		return l
	}
	w := max(p.MinWidth, 4)
	for _, item := range items {
		w = max(w, cell.Width(item.Label)+cell.Width(item.Shortcut)+7)
	}
	l.Width = min(w, p.ScreenWidth)
	if p.MaxWidth > 0 {
		l.Width = min(l.Width, max(p.MaxWidth, 4))
	}
	l.X = min(max(p.X, 0), p.ScreenWidth-l.Width)
	l.Y = min(max(p.Y, 0), p.ScreenHeight-3)
	l.Capacity = max(min(len(items), p.ScreenHeight-l.Y-2), 0)
	if p.MaxRows > 0 {
		l.Capacity = min(l.Capacity, p.MaxRows)
	}
	selected := min(max(p.Selected, 0), max(len(items)-1, 0))
	l.Offset = max(selected-l.Capacity+1, 0)
	inner := l.Width - 2
	l.Rows = append(l.Rows, styles.Surface.Border.Render("╭"+strings.Repeat("─", inner)+"╮"))
	for i := l.Offset; i < min(l.Offset+l.Capacity, len(items)); i++ {
		item := items[i]
		if item.Separator {
			l.Rows = append(l.Rows, styles.Surface.Border.Render("├"+strings.Repeat("─", inner)+"┤"))
			continue
		}
		style := styles.Surface.Normal
		if !item.Enabled && !item.Info {
			style = styles.Disabled
		} else if i == selected {
			style = styles.Surface.Selected
		}
		prefix := "  "
		if item.Checked {
			prefix = "✓ "
		}
		label := prefix + item.Label
		shortcutWidth := cell.Width(item.Shortcut)
		if shortcutWidth > 0 && inner-shortcutWidth-1 >= 3 {
			label = cell.Line(label, inner-shortcutWidth-1) + " " + item.Shortcut
		}
		l.Rows = append(l.Rows, styles.Surface.Border.Render("│")+Row(style, label, inner)+styles.Surface.Border.Render("│"))
	}
	l.Rows = append(l.Rows, styles.Surface.Border.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return l
}

func (l MenuLayout) Contains(x, y int) bool {
	return len(l.Rows) > 0 && x >= l.X && x < l.X+l.Width && y >= l.Y && y < l.Y+len(l.Rows)
}

// Hit excludes the frame, separators, information and disabled commands.
func (l MenuLayout) Hit(x, y int) int {
	if x <= l.X || x >= l.X+l.Width-1 || y <= l.Y || y >= l.Y+len(l.Rows)-1 {
		return -1
	}
	i := l.Offset + y - l.Y - 1
	if i < 0 || i >= len(l.items) || !l.items[i].Enabled || l.items[i].Separator || l.items[i].Info {
		return -1
	}
	return i
}

// MenuNext finds an enabled command starting at the candidate row, wrapping.
func MenuNext(items []MenuItem, start, delta int) int {
	if delta >= 0 {
		delta = 1
	} else {
		delta = -1
	}
	for i := range items {
		row := ((start+i*delta)%len(items) + len(items)) % len(items)
		if items[row].Enabled && !items[row].Separator && !items[row].Info {
			return row
		}
	}
	return 0
}
