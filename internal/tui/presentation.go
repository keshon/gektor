package tui

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
)

func (m *Model) syncPresentation() {
	if m.workflows != nil {
		m.workflows.SetSurface(m.workflowSurface())
	}
	var props screen.ControlProps
	if m.workflowActive() {
		props = m.workflows.Presentation()
		if ws := m.launch.Env.WS; ws != nil {
			props.Identity = filepath.Base(ws.Root())
		}
		if m.chrome.Dialog != nil || m.chrome.Header.Focused {
			props.Actions = nil
		}
	} else {
		props = m.agent.Presentation()
	}
	props.Width, props.Height = m.termW, m.termH
	props.Menus = m.menus(props.Menus)
	m.chrome.Props = props
	if m.chrome.Dialog != nil && (m.chrome.Dialog.Form != nil || m.chrome.Dialog.Table != nil) {
		m.chrome.Props.Actions = m.chrome.DialogActions()
	}
}
func (m *Model) menus(localMenus []screen.Menu) []screen.Menu {
	idle := m.agent.Status().Idle && (m.workflows == nil || !m.workflows.State().Busy)
	app := screen.Menu{ID: "app", Label: "GEKTOR", Items: []screen.MenuItem{
		{Text: "Agent", Command: "screen:agent", Enabled: idle, Checked: !m.workflowActive()},
		{Text: "Workflows", Command: "screen:workflows", Enabled: idle, Checked: m.workflowActive()}, {},
		{Text: "Change workspace…", Command: "workspace", Enabled: idle && m.launch.Application != nil}, {},
		{Text: "Quit", Command: "quit", Enabled: true, Shortcut: "Ctrl+Q"},
	}}
	groups := []screen.Menu{app}
	groups = append(groups, localMenus...)
	return append(groups, m.applicationMenus(idle)...)
}
func (m *Model) workflowSurface() screen.Surface {
	inset := 0
	if m.termW >= 40 && m.termH >= 14 {
		inset = 1
	}
	bottom := inset > 0
	return screen.Surface{Area: pane.Rect{Y: 1, Width: m.termW, Height: max(m.termH-1-m.chrome.ActionBarRows(), 1)}, Inset: inset, SplitWidth: screen.SidePaneWidth, Outlined: m.outlined && inset > 0, BottomEdge: bottom, HeaderFocused: m.chrome.Header.Focused, Styles: screen.DefaultSurfaceStyles()}
}

// View composes either feature with shared controls and terminal modes.
func (m *Model) View() tea.View {
	var view tea.View
	if m.workflowActive() {
		view = m.workflows.View()
	} else {
		view = m.agent.View()
	}
	view = m.chrome.Compose(view)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.ReportFocus = true
	return view
}
