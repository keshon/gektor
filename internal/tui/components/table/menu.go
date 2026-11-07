package table

import (
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

func splitActions(actions []Action) (primary, overflow []Action) {
	for _, action := range actions {
		if action.Overflow {
			overflow = append(overflow, action)
		} else {
			primary = append(primary, action)
		}
	}
	if len(overflow) > 0 {
		at := len(primary)
		if at > 0 && primary[at-1].ID == "close" {
			at--
		}
		primary = append(primary, Action{})
		copy(primary[at+1:], primary[at:])
		primary[at] = Action{ID: "more", Label: "More…", Key: "m", Enabled: true}
	}
	return primary, overflow
}

func menuItems(actions []Action) []popup.MenuItem {
	items := make([]popup.MenuItem, len(actions))
	for i, action := range actions {
		items[i] = popup.MenuItem{Label: action.Label, Shortcut: action.Key, Enabled: action.Enabled, Destructive: action.Destructive}
	}
	return items
}

func (m *Model) activate(id string, p Props) string {
	if id != "more" {
		return id
	}
	_, actions := splitActions(p.Actions)
	m.MoreOpen = true
	m.moreRow = popup.MenuNext(menuItems(actions), 0, 1)
	m.SyncFocus(true)
	return ""
}

func (m *Model) menuKey(msg tea.Msg, l Layout) string {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return ""
	}
	switch key.String() {
	case "esc", "left", "m":
		m.MoreOpen = false
		m.SyncFocus(true)
	case "up", "shift+tab":
		m.moreRow = popup.MenuNext(menuItems(l.MenuActions), m.moreRow-1, -1)
	case "down", "tab":
		m.moreRow = popup.MenuNext(menuItems(l.MenuActions), m.moreRow+1, 1)
	case "enter", "space":
		if m.moreRow >= 0 && m.moreRow < len(l.MenuActions) && l.MenuActions[m.moreRow].Enabled {
			m.MoreOpen = false
			m.SyncFocus(true)
			return l.MenuActions[m.moreRow].ID
		}
	default:
		for _, action := range l.MenuActions {
			if key.String() == action.Key && action.Enabled {
				m.MoreOpen = false
				m.SyncFocus(true)
				return action.ID
			}
		}
	}
	return ""
}

func (m *Model) menuMouse(msg tea.MouseMsg, l Layout, x, y int) string {
	mouse := msg.Mouse()
	if l.Menu.Contains(x, y) {
		if mouse.Button == tea.MouseWheelUp || mouse.Button == tea.MouseWheelDown {
			delta := 1
			if mouse.Button == tea.MouseWheelUp {
				delta = -1
			}
			m.moreRow = popup.MenuNext(menuItems(l.MenuActions), m.moreRow+delta, delta)
			return ""
		}
	}
	if _, click := msg.(tea.MouseClickMsg); !click || mouse.Button != tea.MouseLeft {
		return ""
	}
	if !l.Menu.Contains(x, y) {
		m.MoreOpen = false
		m.SyncFocus(true)
		return ""
	}
	if i := l.Menu.Hit(x, y); i >= 0 {
		m.MoreOpen = false
		m.SyncFocus(true)
		return l.MenuActions[i].ID
	}
	return ""
}
