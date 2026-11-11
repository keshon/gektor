package agent

import (
	"path/filepath"

	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
)

func (m *Model) controlProps() screen.ControlProps {
	left, top, width, height := m.dialogArea()
	props := screen.ControlProps{
		Width:          m.termW,
		Height:         m.termH,
		Area:           pane.Rect{X: left, Y: top, Width: width, Height: height},
		Inset:          m.promptOffset(),
		Ready:          m.ready,
		HasApplication: m.application != nil,
		Evaluating:     m.evaluation != nil,
		Styles:         m.styles,
		Blocked:        m.sessions != nil || m.state == stAsk || m.state == stPermission,
	}
	if m.chrome.Dialog != nil && m.chrome.Dialog.Help {
		for _, section := range m.dialogHelpSections() {
			props.Help = append(props.Help, screen.HelpSection{Title: section.title, Rows: section.rows})
		}
	}
	identity := m.activeSessionTitle()
	if m.stateFile != "" {
		identity = filepath.Base(filepath.Dir(m.stateFile)) + " · " + identity
	}
	if m.ws != nil {
		identity = filepath.Base(m.ws.Root()) + " · " + identity
	}
	props.Identity = identity
	return props
}

func (m *Model) decorateMenu(items []screen.MenuItem) []screen.MenuItem {
	rows := append([]screen.MenuItem(nil), items...)
	for i := range rows {
		_, rows[i].Checked = m.menuToggle(rows[i].Command)
		rows[i].Shortcut = menuShortcut(rows[i].Command)
	}
	return rows
}

// Presentation supplies current menu data, screen identity, and overlay bounds.
// Selection and overlay state belong to the injected application controls.
func (m *Model) Presentation() screen.ControlProps {
	props := m.controlProps()
	for _, group := range m.menuDefinitions() {
		group.Items = m.decorateMenu(m.menuItems(group.ID))
		props.Menus = append(props.Menus, group)
	}
	props.Badges = m.headerBadges()
	for _, id := range []string{"mode", "activity", "model", "context", "run", "steering"} {
		props.BadgeMenus = append(props.BadgeMenus, screen.Menu{ID: id, Items: m.decorateMenu(m.badgeItems(id))})
	}
	props.Actions = m.footerActions()
	return props
}
