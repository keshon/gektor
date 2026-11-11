package table

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

type Hit struct {
	X0, X1, Y int
	Row       int
	Action    string
}

type Layout struct {
	Rows                                 []string
	Hits                                 []Hit
	Width, Capacity, BodyTop, BodyHeight int
	Actions                              []Action
	Menu                                 popup.MenuLayout
	MenuActions                          []Action
}

func (m *Model) Layout(p Props) Layout {
	width := popup.ModalWidth(p.Width)
	inner := max(width-4, 1)
	l := Layout{Width: width, Actions: p.Actions}
	l.Actions, l.MenuActions = splitActions(p.Actions)
	buttonRows, buttonHits := m.actionRows(l.Actions, inner, p)
	l.Rows = popup.Header(p.Title, inner, p.Styles.Title)
	l.Hits = append(l.Hits, Hit{
		X0:     max(inner-3, 0),
		X1:     inner,
		Y:      0,
		Action: "close",
	})
	if p.Description != "" {
		lines := strings.Split(ansi.Wrap(p.Description, inner, ""), "\n")
		free := max(p.Height-9-len(buttonRows), 0)
		count := min(1, free)
		if p.Height >= 22 {
			count = min(3, free)
		}

		for _, line := range lines[:min(len(lines), count)] {
			l.Rows = append(l.Rows, p.Styles.Muted.Render(cell.Line(line, inner)))
		}

		if p.Height >= 22 && free > count {
			l.Rows = append(l.Rows, "")
		}
	}

	if !p.List {
		m.Search.SetWidth(max(inner-cell.Width(m.Search.Prompt)-2, 1))
		l.Hits = append(l.Hits, Hit{
			Y:      len(l.Rows),
			X1:     inner,
			Action: "filter",
		})
		l.Rows = append(l.Rows, cell.Line(m.Search.View(), inner))
		labels := []string{}

		for _, column := range p.Columns {
			labels = append(labels, column.Label)
		}

		l.Rows = append(l.Rows, p.Styles.Muted.Render("  "+columns(labels, p.Columns, inner-4)))
	}

	// Frame edges, footer divider/hint and action rows stay outside scrolling.
	footerRows, rowHeight, trailingGap := 2, 1, 0
	if p.List {
		footerRows, rowHeight, trailingGap = 1, 3, 1
	}

	l.Capacity = max((p.Height-2-len(l.Rows)-footerRows-len(buttonRows)+trailingGap)/rowHeight, 1)
	if len(l.Rows)+l.Capacity*rowHeight-trailingGap+footerRows+len(buttonRows)+2 > p.Height {
		l.Actions = []Action{{
			ID:      "close",
			Label:   "Close",
			Key:     "esc",
			Enabled: true,
		}}
		l.Rows = popup.Header(p.Title, inner, p.Styles.Title)
		l.Rows = append(l.Rows, cell.Line("Enlarge the terminal to use this table.", inner))
		l.Hits = l.Hits[:1]
		l.Capacity = 0
		m.MoreOpen = false
		m.Focus = 2
		m.SyncFocus(true)
		buttonRows, buttonHits = m.actionRows(l.Actions, inner, p)
	} else {
		l.BodyTop = len(l.Rows)
		rows := m.filtered()
		m.Clamp(len(rows), l.Capacity)
		body := []string{}

		for i := m.Offset; i < min(m.Offset+l.Capacity, len(rows)); i++ {
			style, mark := p.Surface.Normal, "  "
			if i == m.Cursor {
				style, mark = p.Surface.Selected, "› "
			}

			l.Hits = append(l.Hits, Hit{
				X1:     max(inner-2, 1),
				Y:      len(l.Rows) + len(body),
				Row:    i,
				Action: "row",
			})
			body = append(body, style.Render(cell.Line(mark+columns(rows[i].Cells, p.Columns, inner-4), max(inner-2, 1))))
			if p.List {
				l.Hits = append(l.Hits, Hit{
					X1:     max(inner-2, 1),
					Y:      len(l.Rows) + len(body),
					Row:    i,
					Action: "row",
				})
				body = append(body, style.Render(cell.Line("   "+rows[i].Detail, inner-2)))
				if i+1 < min(m.Offset+l.Capacity, len(rows)) {
					body = append(body, cell.Line("", inner-2))
				}
			}
		}

		if len(rows) == 0 {
			message := p.EmptyMessage
			if message == "" {
				message = "No items"
			}

			if m.Search.Value() != "" {
				message = "No matches"
			}

			body = append(body, p.Styles.Muted.Render(cell.Line(message, inner-2)))
		}

		l.BodyHeight = len(body)
		l.Rows = append(l.Rows, popup.ScrollRows(body, inner, m.Offset*rowHeight, max(len(rows)*rowHeight-trailingGap, 0), p.Surface.Normal)...)
	}

	shortcuts := []popup.Shortcut{{Key: "Esc", Action: "close"}, {Key: "/", Action: "filter"}, {Key: "Tab", Action: "controls"}}

	for _, action := range p.Actions {
		if action.ID == p.DefaultAction && action.Enabled {
			shortcuts = append([]popup.Shortcut{{Key: "Enter", Action: strings.ToLower(action.Label)}}, shortcuts...)
			break
		}
	}

	if l.Capacity == 0 {
		shortcuts = []popup.Shortcut{{Key: "Esc/Enter", Action: "close"}}
	}

	if m.MoreOpen {
		shortcuts = []popup.Shortcut{{Key: "Esc", Action: "back"}, {Key: "↑↓", Action: "choose"}, {Key: "Enter", Action: "select"}}
	}

	hint := popup.Shortcuts(inner, popup.ShortcutStyles{Key: p.Styles.Normal.Bold(true), Action: p.Styles.Muted}, shortcuts...)
	if p.List {
		l.Rows = append(l.Rows, strings.Repeat("─", inner))
	} else {
		l.Rows = append(l.Rows, popup.Footer(inner, hint, p.Styles.Muted)...)
	}

	for _, hit := range buttonHits {
		hit.Y += len(l.Rows)
		l.Hits = append(l.Hits, hit)
	}

	l.Rows = append(l.Rows, buttonRows...)
	if m.MoreOpen && len(l.MenuActions) > 0 {
		items := menuItems(l.MenuActions)
		x, y := 0, 2

		for _, hit := range l.Hits {
			if hit.Action == "more" {
				x, y = hit.X0, max(2, hit.Y-len(items)-2)
			}
		}

		l.Menu = popup.Menu(items, popup.MenuProps{
			X:            x,
			Y:            y,
			ScreenWidth:  inner,
			ScreenHeight: len(l.Rows),
			Selected:     m.moreRow,
		}, popup.MenuStyles{
			Surface:  p.Surface,
			Disabled: p.Styles.Disabled.Inherit(p.Surface.Normal),
			Danger:   p.Styles.Error.Inherit(p.Surface.Normal),
		})

		for i := range l.Rows {
			l.Rows[i] = cell.Line(l.Rows[i], inner)
		}

		l.Rows = strings.Split(popup.Overlay(strings.Join(l.Rows, "\n"), l.Menu.Rows, l.Menu.X, l.Menu.Y, l.Menu.Width, inner), "\n")
	}

	return l
}

func columns(cells []string, columns []Column, width int) string {
	if width < 46 {
		visible, values := []Column{}, []string{}

		for i, column := range columns {
			if !column.HideNarrow {
				visible = append(visible, column)
				value := ""
				if i < len(cells) {
					value = cells[i]
				}

				values = append(values, value)
			}
		}

		columns, cells = visible, values
	}

	remaining := max(width-2*(len(columns)-1), 1)
	widths := make([]int, len(columns))

	for i, column := range columns {
		if column.Width > 0 {
			widths[i] = min(column.Width, max(remaining-(len(columns)-i-1)*3, 1))
			remaining -= widths[i]
		}
	}

	for i := range widths {
		if widths[i] == 0 {
			widths[i] = max(remaining, 1)
			break
		}
	}

	parts := []string{}

	for i, width := range widths {
		value := ""
		if i < len(cells) {
			value = strings.Join(strings.Fields(cells[i]), " ")
		}

		parts = append(parts, cell.Line(value, max(width, 1)))
	}

	return cell.Line(strings.Join(parts, "  "), width)
}

// actionRows uses the same actions for keyboard and pointer controls.
func (m *Model) actionRows(actions []Action, width int, p Props) ([]string, []Hit) {
	if !p.List {
		return m.buttons(actions, width, p.Styles)
	}

	rows, hits := []string{}, []Hit{}
	row, x := "", 0

	for i, action := range actions {
		key := popup.KeyLabel(action.Key)
		if key == "DELETE" {
			key = "DEL"
		}

		label := key + " " + action.Label
		length := cell.Width(label)
		if x > 0 && x+2+length > width {
			rows = append(rows, cell.Line(row, width))
			row, x = "", 0
		}

		if x > 0 {
			row += "  "
			x += 2
		}

		style := p.Styles.Normal
		if !action.Enabled {
			style = p.Styles.Disabled
		} else if m.Focus == i+2 {
			style = p.Styles.Focus
		}

		row += popup.Shortcuts(min(length, width), popup.ShortcutStyles{Key: style.Bold(true), Action: style}, popup.Shortcut{Key: key, Action: action.Label})
		hits = append(hits, Hit{
			X0:     x,
			X1:     min(x+length, width),
			Y:      len(rows),
			Action: action.ID,
		})
		x += length
	}

	return append(rows, cell.Line(row, width)), hits
}

func (m *Model) buttons(actions []Action, width int, styles form.Styles) ([]string, []Hit) {
	rows, hits := []string{}, []Hit{}
	row, x := "", 0

	for i, action := range actions {
		label := cell.Truncate("[ "+action.Label+" ]", width)
		length := cell.Width(label)
		if x > 0 && x+2+length > width {
			rows = append(rows, row)
			row, x = "", 0
		}

		if x > 0 {
			row += "  "
			x += 2
		}

		row += form.Button(label, m.Focus == i+2, action.Enabled, action.Destructive, styles)
		hits = append(hits, Hit{
			X0:     x,
			X1:     x + length,
			Y:      len(rows),
			Action: action.ID,
		})
		x += length
	}

	rows = append(rows, row)

	for y, row := range rows {
		padding := max(width-cell.Width(row), 0)
		rows[y] = strings.Repeat(" ", padding) + row

		for i := range hits {
			if hits[i].Y == y {
				hits[i].X0 += padding
				hits[i].X1 += padding
			}
		}
	}

	return rows, hits
}
