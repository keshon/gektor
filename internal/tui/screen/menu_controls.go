package screen

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	cellui "github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

// HeaderItems wraps informational rows while keeping command rows intact.
func (m *Controls) HeaderItems() []MenuItem {
	var rows []MenuItem
	items := m.RawHeaderItems()
	labelWidth := 0
	for _, item := range items {
		if label, _, ok := strings.Cut(item.Text, ": "); ok && item.Command == "" {
			labelWidth = max(labelWidth, lipgloss.Width(label+":"))
		}
	}
	for _, item := range items {
		if label, value, ok := strings.Cut(item.Text, ": "); ok && item.Command == "" {
			item.Text = cellui.Line(label+":", labelWidth) + "  " + value
		}
		if item.Command != "" {
			rows = append(rows, item)
			continue
		}
		for _, text := range cellui.Wrap(item.Text, min(58, m.Props.Width)-4) {
			rows = append(rows, MenuItem{Text: text})
		}
	}
	return rows
}

// CloseHeader returns input ownership from menus and badges to the underlying screen.
func (m *Controls) CloseHeader() {
	m.Header.Focused = false
	m.Header.Menu = ""
	m.Header.Global = false
}

// HeaderKey updates selection and returns an activated command for the shell to dispatch.
func (m *Controls) HeaderKey(msg tea.KeyPressMsg) string {
	m.NormalizeHeader()
	switch msg.String() {
	case "ctrl+q", "f10":
		return "quit"
	case "ctrl+c":
		return "interrupt"
	case "esc":
		m.CloseHeader()
		return ""
	case "f9":
		m.CloseHeader()
		return ""
	case "left", "right":
		delta := 1
		if msg.String() == "left" {
			delta = -1
		}
		badges := m.HeaderNavigation()
		if len(badges) == 0 {
			return ""
		}
		m.Header.Index = (m.Header.Index + delta + len(badges)) % len(badges)
		if m.Header.Menu != "" {
			m.Header.Menu = badges[m.Header.Index].ID
			m.Header.Row = m.HeaderActionRow(0, 1)
		}
	case "up", "down":
		delta := 1
		if msg.String() == "up" {
			delta = -1
		}
		if m.Header.Global {
			m.Header.Row = m.HeaderActionRow(m.Header.Row+delta, delta)
		} else {
			m.Header.Row = min(max(m.Header.Row+delta, 0), max(len(m.HeaderItems())-1, 0))
		}
	case "enter":
		if m.Header.Menu == "" {
			badges := m.HeaderNavigation()
			if len(badges) == 0 {
				return ""
			}
			m.Header.Menu = badges[min(m.Header.Index, len(badges)-1)].ID
			m.Header.Row = m.HeaderActionRow(0, 1)
			return ""
		}
		items := m.HeaderItems()
		if m.Header.Row >= len(items) {
			return ""
		}
		item := items[m.Header.Row]
		if item.Enabled && item.Command != "" {
			return m.ActivateMenuItem(item.Command)
		}
	}
	return ""
}

// HeaderMenuItems converts current menu data into popup rows with availability and check marks.
func (m *Controls) HeaderMenuItems() []popup.MenuItem {
	var items []popup.MenuItem
	for _, item := range m.HeaderItems() {
		items = append(items, popup.MenuItem{Label: item.Text, Shortcut: item.Shortcut,
			Enabled: item.Enabled && item.Command != "", Checked: item.Checked,
			Separator: item.Text == "" && item.Command == "", Info: item.Text != "" && item.Command == ""})
	}
	return items
}

// HeaderMenuLayout measures the attached menu for rendering and pointer input.
func (m *Controls) HeaderMenuLayout() popup.MenuLayout {
	x := 0
	for _, b := range m.HeaderNavigation() {
		if b.ID == m.Header.Menu || b.ID == "overflow" {
			x = b.Start
			if !m.Header.Global {
				x += m.Props.Inset
			}
			break
		}
	}
	normal, border, selected := PopupStyles()
	return popup.Menu(m.HeaderMenuItems(), popup.MenuProps{X: x, Y: m.HeaderTop(), ScreenWidth: m.Props.Width,
		ScreenHeight: m.Props.Height - m.ActionBarRows(), MinWidth: 24, MaxWidth: 58,
		MaxRows: max(m.Props.Height-m.FooterRows()-5, 1), Selected: m.Header.Row},
		popup.MenuStyles{Surface: popup.Styles{Normal: normal, Border: border, Selected: selected},
			Disabled: normal.Foreground(lipgloss.Color(Color(ColorDisabledText)))})
}

// HeaderGeometry returns the attached menu bounds and visible row range.
func (m *Controls) HeaderGeometry() (x, width, capacity, offset int) {
	l := m.HeaderMenuLayout()
	return l.X, l.Width, l.Capacity, l.Offset
}

// OverlayHeader paints the attached menu after the underlying screen and inspector.
func (m *Controls) OverlayHeader(background string) string {
	l := m.HeaderMenuLayout()
	if len(l.Rows) == 0 {
		return background
	}
	return popup.Overlay(background, l.Rows, l.X, l.Y, l.Width, m.Props.Width)
}

// HeaderMouse updates menu focus and returns any command selected by a pointer press.
func (m *Controls) HeaderMouse(msg tea.MouseMsg) (bool, string) {
	mouse := msg.Mouse()
	if mouse.Button == tea.MouseLeft && IsMouseClick(msg) && mouse.Y == 0 {
		for i, cell := range m.MenuCells() {
			if mouse.X >= cell.Start && mouse.X < cell.End {
				if !cell.Action.Enabled {
					return true, ""
				}
				m.Header = MenuState{Focused: true, Global: true, Index: i, Menu: cell.Action.Name}
				m.Header.Row = m.HeaderActionRow(0, 1)
				return true, ""
			}
		}
	}

	if m.Dialog != nil || m.Props.Blocked {
		return false, ""
	}
	if m.ActionBarVisible() && mouse.Y >= m.Props.Height-m.ActionBarRows() {
		return false, ""
	}

	if m.Header.Menu != "" {
		layout := m.HeaderMenuLayout()
		if mouse.Button == tea.MouseLeft && IsMouseClick(msg) {
			if row := layout.Hit(mouse.X, mouse.Y); row >= 0 {
				m.Header.Row = row
				cmd := m.HeaderKey(tea.KeyPressMsg{Code: tea.KeyEnter})
				return true, cmd
			}
			if !layout.Contains(mouse.X, mouse.Y) {
				m.CloseHeader()
			}
		}
		if layout.Contains(mouse.X, mouse.Y) {
			if mouse.Button == tea.MouseWheelUp {
				m.Header.Row = m.HeaderActionRow(m.Header.Row-1, -1)
			}
			if mouse.Button == tea.MouseWheelDown {
				m.Header.Row = m.HeaderActionRow(m.Header.Row+1, 1)
			}
		}
		return true, ""
	}

	if m.Header.Focused && mouse.Button == tea.MouseLeft && IsMouseClick(msg) {
		m.CloseHeader()
		return true, ""
	}
	return false, ""
}

// NormalizeHeader clamps retained selection after the menu contents change.
func (m *Controls) NormalizeHeader() {
	m.Header.Index = min(max(m.Header.Index, 0), len(m.HeaderNavigation())-1)
	m.Header.Row = min(max(m.Header.Row, 0), max(len(m.HeaderItems())-1, 0))
}

// MenuCells measures the application menu row, respecting blocked input ownership.
func (m *Controls) MenuCells() []ActionCell {
	enabled := m.Dialog == nil && !m.Props.Blocked
	x := 1
	var cells []ActionCell
	menus := m.VisibleMenus()
	for _, menu := range menus {
		if x >= m.Props.Width-1 {
			break
		}
		end := min(x+lipgloss.Width(menu.Label)+2, m.Props.Width-1)
		cells = append(cells, ActionCell{Action: Action{Name: menu.ID, Label: menu.Label, Enabled: enabled}, Start: x, End: end})
		x = end + 1
	}
	return cells
}

// HeaderNavigation returns application menus or the active screen badges according to focus.
func (m *Controls) HeaderNavigation() []Badge {
	if !m.Header.Global {
		return m.Props.Badges
	}
	var badges []Badge
	for _, cell := range m.MenuCells() {
		badges = append(badges, Badge{ID: cell.Action.Name, Label: cell.Action.Label, Start: cell.Start, End: cell.End})
	}
	return badges
}

// MenuLine fits application menus and screen identity onto one terminal row.
func (m *Controls) MenuLine() string {
	normal, _, selected := PopupStyles()
	line := ""
	for i, cell := range m.MenuCells() {
		line += normal.Render(strings.Repeat(" ", max(cell.Start-lipgloss.Width(line), 0)))
		label := cellui.Line(" "+cell.Action.Label, cell.End-cell.Start)
		if !cell.Action.Enabled {
			label = normal.Foreground(lipgloss.Color(Color(ColorDisabledText))).Render(label)
		} else if m.Header.Focused && m.Header.Global && m.Header.Index == i {
			label = selected.Render(label)
		} else {
			label = normal.Render(label)
		}
		line += label
	}
	identity := m.Props.Identity
	available := m.Props.Width - lipgloss.Width(line) - 3
	if available >= 12 {
		identity = strings.TrimSpace(cellui.Line(identity, available))
		line += normal.Render(strings.Repeat(" ", max(m.Props.Width-lipgloss.Width(line)-lipgloss.Width(identity)-1, 0)))
		line += normal.Foreground(lipgloss.Color(Color(ColorSecondaryText))).Render(identity)
	}
	return line + normal.Render(strings.Repeat(" ", max(m.Props.Width-lipgloss.Width(line), 0)))
}

// HeaderTop anchors global menus below the menu row and badge popups below the status row.
func (m *Controls) HeaderTop() int {
	if m.Header.Global {
		return 1
	}
	return 3
}

// HeaderActionRow finds the next selectable command while skipping disabled and informational rows.
func (m *Controls) HeaderActionRow(start, delta int) int {
	items := m.HeaderItems()
	if len(items) == 0 {
		return 0
	}
	if !m.Header.Global {
		return min(max(start, 0), len(items)-1)
	}
	return popup.MenuNext(m.HeaderMenuItems(), start, delta)
}

// VisibleMenus keeps clipped groups reachable through an overflow menu.
func (m *Controls) VisibleMenus() []Menu {
	menus := m.Props.Menus
	width := 1
	for _, menu := range menus {
		width += lipgloss.Width(menu.Label) + 3
	}
	if width <= m.Props.Width {
		return menus
	}
	used, count := 1, 0
	for _, menu := range menus {
		if used+lipgloss.Width(menu.Label)+3+lipgloss.Width("More…")+3 > m.Props.Width {
			break
		}
		used += lipgloss.Width(menu.Label) + 3
		count++
	}
	return append(menus[:count:count], Menu{ID: "overflow", Label: "More…"})
}

// ActionBarVisible reports whether terminal dimensions leave room for the shortcut row.
func (m *Controls) ActionBarVisible() bool { return m.Props.Width >= 40 && m.Props.Height >= 14 }

// ActionBarRows returns the number of terminal rows occupied by shortcuts.
func (m *Controls) ActionBarRows() int {
	if !m.ActionBarVisible() {
		return 0
	}

	return 1
}

// FooterRows includes the pane edge above the shortcut row.
func (m *Controls) FooterRows() int {
	if !m.ActionBarVisible() {
		return 0
	}
	return m.ActionBarRows() + 1
}

// ToolbarLayout measures complete shortcuts for both rendering and click dispatch.
func (m *Controls) ToolbarLayout() pane.ToolbarLayout {
	var items []pane.ToolAction
	for _, action := range m.Props.Actions {
		compactKey := ""
		if strings.HasPrefix(action.Name, "Ctrl+") {
			compactKey = "^" + strings.TrimPrefix(action.Name, "Ctrl+")
		}
		items = append(items, pane.ToolAction{Key: action.Name, CompactKey: compactKey, Label: action.Label, CompactLabel: action.Short, Enabled: action.Enabled})
	}
	style := func(fg, bg string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(Color(fg))).Background(lipgloss.Color(Color(bg)))
	}
	return pane.Toolbar(m.Props.Width, items, pane.ToolbarStyles{
		Key:           style(ColorNumberText, ColorNumberBackground).Bold(true),
		Label:         style(ColorSelectedText, ColorActionBackground),
		DisabledKey:   style(ColorDisabledText, ColorDisabledNumber),
		DisabledLabel: style(ColorDisabledText, ColorDisabledLabel),
		Gap:           lipgloss.NewStyle().Background(lipgloss.Color(CurrentPalette().Background)),
	})
}

// FooterCells pairs displayed toolbar regions with their corresponding actions.
func (m *Controls) FooterCells() []ActionCell {
	actions := m.Props.Actions
	var cells []ActionCell
	for i, item := range m.ToolbarLayout().Cells {
		cells = append(cells, ActionCell{Action: actions[i], Row: item.Bounds.Y, Start: item.Bounds.X, End: item.Bounds.X + item.Bounds.Width})
	}
	return cells
}

// ActionBar renders the measured shortcut row.
func (m *Controls) ActionBar() string { return m.ToolbarLayout().Row }

// RawHeaderItems selects menu, overflow, or badge data without wrapping its labels.
func (m *Controls) RawHeaderItems() []MenuItem {
	if m.Header.Menu == "overflow" {
		visible := map[string]bool{}
		for _, g := range m.VisibleMenus() {
			visible[g.ID] = true
		}
		var rows []MenuItem
		for _, g := range m.Props.Menus {
			if !visible[g.ID] {
				rows = append(rows, MenuItem{Text: g.Label, Command: "menu:" + g.ID, Enabled: true})
			}
		}
		return rows
	}
	for _, g := range m.Props.Menus {
		if g.ID == m.Header.Menu {
			return g.Items
		}
	}
	for _, g := range m.Props.BadgeMenus {
		if g.ID == m.Header.Menu {
			return g.Items
		}
	}
	return nil
}

// ActivateMenuItem opens overflow destinations locally and returns feature commands to the shell.
func (m *Controls) ActivateMenuItem(command string) string {
	if strings.HasPrefix(command, "menu:") {
		m.Header.Menu = strings.TrimPrefix(command, "menu:")
		m.Header.Row = m.HeaderActionRow(0, 1)
		return ""
	}
	m.CloseHeader()
	return command
}

// ToolbarKey returns a key only when the pointer hits an enabled shortcut.
func (m *Controls) ToolbarKey(x int) (tea.KeyPressMsg, bool) {
	if i := m.ToolbarLayout().Hit(x, 0); i >= 0 {
		a := m.Props.Actions[i]
		return tea.KeyPressMsg{Code: a.Key, Mod: a.Mod}, true
	}
	return tea.KeyPressMsg{}, false
}
