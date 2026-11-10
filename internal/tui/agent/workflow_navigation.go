package agent

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/screen"
)

type workflowQuit struct{}

func (m *Model) workflowActions() []footerAction {
	var actions []footerAction
	for _, a := range m.workflows.Actions() {
		actions = append(actions, footerAction{key: a.Key, mod: a.Mod, name: a.Name, label: a.Label, short: a.Short, enabled: a.Enabled})
	}
	return actions
}

func (m *Model) workflowSurface() screen.Surface {
	return screen.Surface{
		Area:  m.screenPanes(false).Main,
		Inset: m.promptOffset(), SplitWidth: inspectorSplitWidth,
		Outlined: !m.minimal && m.promptOffset() > 0, BottomEdge: m.actionBarVisible(),
		HeaderFocused: m.header.focused,
		Styles: screen.SurfaceStyles{
			Panels: m.paneStyles(), Shortcuts: m.shortcutStyles(),
			Graph: m.graphStyles(), Notice: m.noticeStyles(),
			Muted:  m.styles.dim,
			Accent: m.styles.hunk, Error: m.styles.err,
		},
	}
}

func (m *Model) workflowActive() bool { return m.workflows != nil && m.workflows.State().Active }

func (m *Model) workflowMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	if m.actionBarVisible() && mouse.Y == m.termH-1 && mouse.Button == tea.MouseLeft && isMouseClick(msg) {
		return m.toolbarClick(mouse.X)
	}
	_, cmd := m.workflows.Update(msg)
	return m, cmd
}

func (m *Model) openWorkflows() {
	if m.state != stDone {
		m.openDialog("Workflows", []string{"Finish or stop the agent turn before opening the editor."})
		return
	}
	m.cancelSessionTitle()
	if m.workflows == nil {
		if m.baseEnv.WS == nil {
			m.openDialog("Workflows", []string{"Select a workspace first."})
			return
		}
		dialogs := m.screenHost()
		cfg := screen.WorkflowServices{
			WS: m.baseEnv.WS, Client: m.baseEnv.Client, Policy: m.baseEnv.Policy,
			MaxTokens: m.baseEnv.MaxTokens, ContextLimit: m.baseEnv.ContextLimit,
			Temperature: m.baseEnv.Temperature,
		}
		if m.workflowFactory == nil {
			m.openDialog("Workflows", []string{"Workflow screen is unavailable."})
			return
		}
		child, err := m.workflowFactory(m.base, cfg, dialogs, m.workflowSurface)
		if err != nil {
			m.openDialog("Workflows", []string{err.Error()})
			return
		}
		m.workflows = child
	}
	m.workflows.Activate()
	m.inspector = toolInspector{}
	m.fitBottom()
}
func (m *Model) workflowCommand(action string) tea.Cmd {
	if !m.workflowActive() {
		m.openWorkflows()
	}
	if !m.workflowActive() {
		return nil
	}
	return m.workflows.Command(action)
}
func (m *Model) workflowScreen() string {
	content := cellLine(m.menuLine(), m.termW) + "\n" + m.workflows.Content() + "\n" + m.actionBar()
	content = cellFrame(content, m.termW, m.termH)
	if m.dialog != nil {
		content = m.workflowDialog(content)
	}
	if m.header.menu != "" {
		content = m.overlayHeader(content)
	}
	return content
}

// Shared dialogs overlay pane-local content; their hit tests add settingsArea's
// screen origin. Keep that same origin when composing the workflow's full screen.
func (m *Model) workflowDialog(content string) string {
	left, top, width, height := m.settingsArea()
	base := strings.Split(content, "\n")
	pane := make([]string, height)
	for i := range pane {
		if top+i < len(base) {
			pane[i] = cellLine(ansi.Cut(base[top+i], left, left+width), width)
		}
	}
	rows := strings.Split(m.overlayDialog(strings.Join(pane, "\n")), "\n")
	for i, row := range rows {
		y := top + i
		if y >= len(base) || i >= height {
			break
		}
		base[y] = cellLine(ansi.Cut(base[y], 0, left), left) + cellLine(row, width) + ansi.Cut(base[y], left+width, m.termW)
	}
	return cellFrame(strings.Join(base, "\n"), m.termW, m.termH)
}
