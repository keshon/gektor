package tui

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/workflow"
)

func (m *Model) syncPresentation() {
	props := m.agent.Presentation()
	props.Width, props.Height = m.termW, m.termH
	props.Menus = m.menus(props.Menus)
	if m.workflowActive() {
		_, height := m.workflows.CanvasSize()
		props.Area = pane.Rect{X: 1, Y: 4, Width: max(1, m.termW-2), Height: height}
		if ws := m.config.AgentConfig.Env.WS; ws != nil {
			props.Identity = filepath.Base(ws.Root())
		}
		props.Badges = nil
		if m.chrome.Dialog == nil && !m.chrome.Header.Focused && !m.agent.Status().BrowserOpen {
			props.Actions = m.workflows.Actions()
		}
	}
	m.chrome.Props = props
	if m.chrome.Dialog != nil && (m.chrome.Dialog.Form != nil || m.chrome.Dialog.Table != nil) {
		m.chrome.Props.Actions = m.chrome.DialogActions()
	}
}
func (m *Model) menus(agentMenus []screen.Menu) []screen.Menu {
	idle := m.agent.Status().Idle && (m.workflows == nil || !m.workflows.State().Busy)
	app := screen.Menu{ID: "app", Label: "GEKTOR", Items: []screen.MenuItem{
		{Text: "Agent", Command: "screen:agent", Enabled: idle, Checked: !m.workflowActive()},
		{Text: "Workflows", Command: "screen:workflows", Enabled: idle, Checked: m.workflowActive()}, {},
		{Text: "Change workspace…", Command: "workspace", Enabled: idle && m.config.Application != nil}, {},
		{Text: "Quit", Command: "quit", Enabled: true, Shortcut: "Ctrl+Q"},
	}}
	groups := []screen.Menu{app}
	if m.workflowActive() {
		local := workflow.Menus()
		local = append(local, screen.Menu{ID: "execution", Label: "Run"}, screen.Menu{ID: "view", Label: "View"})
		for _, group := range local {
			group.Items = m.workflows.MenuItems(group.ID, m.agent.Status().Idle)
			for i := range group.Items {
				_, group.Items[i].Checked = m.workflows.MenuToggle(group.Items[i].Command)
				group.Items[i].Shortcut = workflow.MenuShortcut(group.Items[i].Command)
			}
			groups = append(groups, group)
		}
	}
	for _, group := range agentMenus {
		if group.ID == "app" {
			continue
		}
		if m.workflowActive() && group.ID != "settings" && group.ID != "help" {
			continue
		}
		if !idle && group.ID == "settings" {
			for i := range group.Items {
				if strings.HasPrefix(group.Items[i].Command, "edit:") {
					group.Items[i].Enabled = false
				}
			}
		}
		groups = append(groups, group)
	}
	return groups
}
func (m *Model) workflowSurface() screen.Surface {
	inset := 0
	if m.termW >= 40 && m.termH >= 14 {
		inset = 1
	}
	bottom := inset > 0
	return screen.Surface{Area: pane.Rect{Y: 1, Width: m.termW, Height: max(m.termH-1-m.chrome.ActionBarRows(), 1)}, Inset: inset, SplitWidth: screen.SidePaneWidth, Outlined: !m.agent.Status().Minimal && inset > 0, BottomEdge: bottom, HeaderFocused: m.chrome.Header.Focused, Styles: screen.DefaultSurfaceStyles()}
}

// View renders the active screen with application-owned menus and terminal modes.
func (m *Model) View() tea.View {
	var view tea.View
	if !m.agent.Status().Ready || !m.workflowActive() {
		view = m.agent.View()
	} else {
		content := cell.Line(m.chrome.MenuLine(), m.termW) + "\n" + m.workflows.Content()
		if m.chrome.ActionBarVisible() {
			content += "\n" + m.chrome.ActionBar()
		}
		content = cell.Frame(content, m.termW, m.termH)
		if m.chrome.Dialog != nil {
			content = m.workflowDialog(content)
		}
		if m.chrome.Header.Menu != "" {
			content = m.chrome.OverlayHeader(content)
		}
		view = tea.NewView(screen.Paint(content))
	}
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.ReportFocus = true
	return view
}
func (m *Model) workflowDialog(content string) string {
	area := m.chrome.Props.Area
	base := strings.Split(content, "\n")
	rows := make([]string, area.Height)
	for i := range rows {
		if area.Y+i < len(base) {
			rows[i] = cell.Line(ansi.Cut(base[area.Y+i], area.X, area.X+area.Width), area.Width)
		}
	}
	overlay := strings.Split(m.chrome.OverlayDialog(strings.Join(rows, "\n")), "\n")
	for i, row := range overlay {
		y := area.Y + i
		if y >= len(base) || i >= area.Height {
			break
		}
		base[y] = cell.Line(ansi.Cut(base[y], 0, area.X), area.X) + cell.Line(row, area.Width) + ansi.Cut(base[y], area.X+area.Width, m.termW)
	}
	return cell.Frame(strings.Join(base, "\n"), m.termW, m.termH)
}
