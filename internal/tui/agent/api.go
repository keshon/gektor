package agent

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/screen"
)

// Status exposes only facts needed by the application shell's routing policy.
type Status struct{ Idle, Ready, Gated, BrowserOpen, Minimal bool }

// Status returns routing flags without exposing the conversation or run.
func (m *Model) Status() Status {
	return Status{Idle: m.state == stDone, Ready: m.ready, Gated: m.state == stAsk || m.state == stPermission, BrowserOpen: m.sessions != nil, Minimal: m.minimal}
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

// Action handles an Agent or settings command chosen by application controls.
func (m *Model) Action(command string) tea.Cmd {
	if strings.HasPrefix(command, "/") {
		_, cmd := m.command(command)
		return cmd
	}
	_, cmd := m.activateMenuAction(command)
	return cmd
}

// Hide releases transient conversation UI while retaining history and the draft.
func (m *Model) Hide() {
	m.cancelSessionTitle()
	m.inspector = toolInspector{}
	if m.sessions != nil {
		m.closeSessions()
	}
	m.fitBottom()
}

// Refresh reconciles focus and control effects after an application interaction.
func (m *Model) Refresh() tea.Cmd {
	m.syncSuggestions()
	m.syncFocus()
	m.queueSettingsDetail()
	m.syncComposerHeight()
	m.resizeFormEditor()
	return m.takeCommands()
}

func (m *Model) request(msg tea.Msg) tea.Cmd { m.requests = append(m.requests, msg); return nil }

// TakeRequests transfers navigation and quit intents to the application once.
func (m *Model) TakeRequests() []tea.Msg { requests := m.requests; m.requests = nil; return requests }
