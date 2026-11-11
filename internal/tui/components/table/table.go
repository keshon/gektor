// Package table provides searchable rows and direct actions without owning
// application services, persistence or permissions.
package table

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
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
