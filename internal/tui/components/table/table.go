// Package table provides searchable rows and direct actions without owning
// application services, persistence or permissions.
package table

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/selection"
)

type Column struct {
	Label      string
	Width      int  // Zero fills the remaining width.
	HideNarrow bool // Optional metadata yields in narrow tables.
}

type Row struct {
	ID     string
	Cells  []string
	Detail string
}

type Action struct {
	ID, Label, Key       string
	Enabled, Destructive bool
	Overflow             bool
}

type Props struct {
	Title, Description string
	EmptyMessage       string
	DefaultAction      string
	Width, Height      int
	// List shows compact cards with a detail row and direct shortcut actions.
	List    bool
	Columns []Column
	Actions []Action
	Styles  form.Styles
	Surface popup.Styles
}

type Model struct {
	Search textinput.Model
	Rows   []Row
	selection.Model
	// Focus is zero for rows, one for the filter, then one per action.
	Focus    int
	MoreOpen bool
	moreRow  int
}

func New(styles textinput.Styles) *Model {
	search := textinput.New()
	search.SetStyles(styles)
	search.Prompt = "Filter: "
	search.Placeholder = "/ to filter"
	search.SetVirtualCursor(true)
	return &Model{Search: search}
}

func (m *Model) filtered() []Row {
	query := strings.ToLower(m.Search.Value())
	rows := []Row{}
	for _, row := range m.Rows {
		if strings.Contains(strings.ToLower(strings.Join(row.Cells, " ")), query) {
			rows = append(rows, row)
		}
	}
	return rows
}

func (m *Model) Selected() string {
	rows := m.filtered()
	if len(rows) == 0 {
		return ""
	}
	return rows[min(max(m.Cursor, 0), len(rows)-1)].ID
}

func (m *Model) SetRows(rows []Row) {
	id := m.Selected()
	m.Rows = rows
	for i, row := range m.filtered() {
		if row.ID == id {
			m.Cursor = i
			return
		}
	}
	m.Clamp(len(m.filtered()), 1)
}

func (m *Model) SyncFocus(active bool) {
	if active && m.Focus == 1 && !m.MoreOpen {
		m.Search.Focus()
	} else {
		m.Search.Blur()
	}
}

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

func (m *Model) Layout(p Props) Layout {
	width := popup.ModalWidth(p.Width)
	inner := max(width-4, 1)
	l := Layout{Width: width, Actions: p.Actions}
	l.Actions, l.MenuActions = splitActions(p.Actions)
	buttonRows, buttonHits := m.actionRows(l.Actions, inner, p)
	l.Rows = popup.Header(p.Title, inner, p.Styles.Title)
	l.Hits = append(l.Hits, Hit{X0: max(inner-3, 0), X1: inner, Y: 0, Action: "close"})
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
		l.Hits = append(l.Hits, Hit{Y: len(l.Rows), X1: inner, Action: "filter"})
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
		l.Actions = []Action{{ID: "close", Label: "Close", Key: "esc", Enabled: true}}
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
			l.Hits = append(l.Hits, Hit{X1: max(inner-2, 1), Y: len(l.Rows) + len(body), Row: i, Action: "row"})
			body = append(body, style.Render(cell.Line(mark+columns(rows[i].Cells, p.Columns, inner-4), max(inner-2, 1))))
			if p.List {
				l.Hits = append(l.Hits, Hit{X1: max(inner-2, 1), Y: len(l.Rows) + len(body), Row: i, Action: "row"})
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
		l.Menu = popup.Menu(items, popup.MenuProps{X: x, Y: y, ScreenWidth: inner, ScreenHeight: len(l.Rows), Selected: m.moreRow}, popup.MenuStyles{Surface: p.Surface, Disabled: p.Styles.Disabled.Inherit(p.Surface.Normal), Danger: p.Styles.Error.Inherit(p.Surface.Normal)})
		for i := range l.Rows {
			l.Rows[i] = cell.Line(l.Rows[i], inner)
		}
		l.Rows = strings.Split(popup.Overlay(strings.Join(l.Rows, "\n"), l.Menu.Rows, l.Menu.X, l.Menu.Y, l.Menu.Width, inner), "\n")
	}
	return l
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
		hits = append(hits, Hit{X0: x, X1: min(x+length, width), Y: len(rows), Action: action.ID})
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
		hits = append(hits, Hit{X0: x, X1: x + length, Y: len(rows), Action: action.ID})
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

// Update returns an action ID. Merely selecting or filtering never activates it.
func (m *Model) Update(msg tea.Msg, p Props) (string, tea.Cmd) {
	l := m.Layout(p)
	if m.MoreOpen {
		return m.menuKey(msg, l), nil
	}
	if l.Capacity == 0 {
		if key, ok := msg.(tea.KeyPressMsg); ok && (key.String() == "esc" || key.String() == "enter" || key.String() == "space") {
			return "close", nil
		}
		return "", nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		text := key.String()
		switch text {
		case "esc":
			if m.Focus == 1 {
				m.Focus = 0
				m.SyncFocus(true)
				return "", nil
			}
			return "close", nil
		case "tab", "shift+tab":
			step := 1
			if text == "shift+tab" {
				step = -1
			}
			m.Focus = (m.Focus + step + len(l.Actions) + 2) % (len(l.Actions) + 2)
			if p.List && m.Focus == 1 {
				m.Focus = (m.Focus + step + len(l.Actions) + 2) % (len(l.Actions) + 2)
			}
			m.SyncFocus(true)
			return "", nil
		case "/":
			if !p.List && m.Focus != 1 {
				m.Focus = 1
				m.SyncFocus(true)
				return "", nil
			}
		case "up", "down", "pgup", "pgdown":
			m.Model.Update(msg, len(m.filtered()), max(l.Capacity, 1))
			return "", nil
		case "home", "ctrl+home":
			if m.Focus != 1 || text == "ctrl+home" {
				m.Cursor = 0
				return "", nil
			}
		case "end", "ctrl+end":
			if m.Focus != 1 || text == "ctrl+end" {
				m.Cursor = max(len(m.filtered())-1, 0)
				return "", nil
			}
		case "enter", "space":
			if m.Focus >= 2 && m.Focus-2 < len(l.Actions) {
				action := l.Actions[m.Focus-2]
				if action.Enabled {
					return m.activate(action.ID, p), nil
				}
				return "", nil
			}
			if text == "enter" {
				m.Focus = 0
				m.SyncFocus(true)
				for _, action := range l.Actions {
					if action.ID == p.DefaultAction && action.Enabled {
						return action.ID, nil
					}
				}
				return "", nil
			}
		}
		if m.Focus != 1 {
			if text == "m" && len(l.MenuActions) > 0 {
				return m.activate("more", p), nil
			}
			for _, action := range p.Actions {
				if text == action.Key && action.Enabled {
					return m.activate(action.ID, p), nil
				}
			}
			return "", nil
		}
	}
	if m.Focus != 1 {
		return "", nil
	}
	previous := m.Search.Value()
	var cmd tea.Cmd
	m.Search, cmd = m.Search.Update(msg)
	if previous != m.Search.Value() {
		m.Cursor, m.Offset = 0, 0
	}
	return "", cmd
}

func (m *Model) Mouse(msg tea.MouseMsg, p Props) string {
	l := m.Layout(p)
	x := max((p.Width-l.Width)/2, 0) + 2
	y := max((p.Height-len(l.Rows)-2)/2, 0) + 1
	mouse := msg.Mouse()
	if m.MoreOpen {
		return m.menuMouse(msg, l, mouse.X-x, mouse.Y-y)
	}
	if mouse.X < x || mouse.X >= x+l.Width-4 || mouse.Y < y || mouse.Y >= y+len(l.Rows) {
		return ""
	}
	if mouse.Button == tea.MouseWheelUp || mouse.Button == tea.MouseWheelDown {
		if l.Capacity > 0 && mouse.Y >= y+l.BodyTop && mouse.Y < y+l.BodyTop+l.BodyHeight {
			step := 1
			if mouse.Button == tea.MouseWheelUp {
				step = -1
			}
			m.Move(step, len(m.filtered()), l.Capacity)
		}
		return ""
	}
	if _, ok := msg.(tea.MouseClickMsg); !ok || mouse.Button != tea.MouseLeft {
		return ""
	}
	row := mouse.Y - y - l.BodyTop
	rowHeight := 1
	if p.List {
		rowHeight = 3
	}
	if l.Capacity > 0 && mouse.X == x+l.Width-5 && row >= 0 && row < l.BodyHeight && len(m.filtered()) > l.Capacity {
		total := len(m.filtered()) * rowHeight
		if p.List {
			total--
		}
		top, size := popup.ScrollThumb(l.BodyHeight, total, m.Offset*rowHeight)
		if row < top {
			m.Move(-l.Capacity, len(m.filtered()), l.Capacity)
		}
		if row >= top+size {
			m.Move(l.Capacity, len(m.filtered()), l.Capacity)
		}
		return ""
	}
	for _, hit := range l.Hits {
		if mouse.Y != y+hit.Y || mouse.X < x+hit.X0 || mouse.X >= x+hit.X1 {
			continue
		}
		switch hit.Action {
		case "row":
			m.Cursor, m.Focus = hit.Row, 0
			m.SyncFocus(true)
			return ""
		case "filter":
			m.Focus = 1
			m.SyncFocus(true)
			return ""
		case "close":
			return "close"
		}
		for _, action := range l.Actions {
			if action.ID == hit.Action && action.Enabled {
				return m.activate(action.ID, p)
			}
		}
	}
	return ""
}
