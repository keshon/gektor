package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
	workflowui "github.com/keshon/gektor/internal/tui/workflow"
)

type workflowQuit struct{}

func (m *model) workflowActions() []footerAction {
	var actions []footerAction
	for _, a := range m.workflows.Actions() {
		actions = append(actions, footerAction{key: a.Key, mod: a.Mod, name: a.Name, label: a.Label, short: a.Short, enabled: a.Enabled})
	}
	return actions
}

func (m *model) workflowSurface() workflowui.Surface {
	return workflowui.Surface{
		Area:  m.screenPanes(false).Main,
		Inset: m.promptOffset(), SplitWidth: inspectorSplitWidth,
		Outlined: !m.minimal && m.promptOffset() > 0, BottomEdge: m.actionBarVisible(),
		HeaderFocused: m.header.focused,
		Styles: workflowui.SurfaceStyles{
			Panels: m.paneStyles(), Shortcuts: m.shortcutStyles(),
			Graph: m.graphStyles(), Notice: m.noticeStyles(),
			Muted:  m.styles.dim,
			Accent: m.styles.hunk, Error: m.styles.err,
		},
	}
}

func (m *model) openScreenForm(f screen.Form) *settings.Model {
	if f.StartOnCancel {
		m.openRemoval(f.Title, f.Hint, f.ApplyLabel, f.Fields, f.Apply)
	} else {
		m.openForm(f.Title, f.Hint, f.ApplyLabel, f.Fields, f.Apply)
	}
	m.dialog.form.Destructive = f.Destructive
	m.dialog.form.Choices = f.Choices
	m.dialog.form.FileFields = f.FileFields
	m.dialog.onClose = f.OnClose
	return &m.dialog.form.Model
}

func (m *model) workflowActive() bool { return m.workflows != nil && m.workflows.State().Active }

func (m *model) workflowMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	if m.actionBarVisible() && mouse.Y == m.termH-1 && mouse.Button == tea.MouseLeft && isMouseClick(msg) {
		return m.toolbarClick(mouse.X)
	}
	_, cmd := m.workflows.Update(msg)
	return m, cmd
}

func (m *model) openWorkflows() {
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
		cfg := workflowui.Config{
			WS: m.baseEnv.WS, Client: m.baseEnv.Client, Policy: m.baseEnv.Policy,
			MaxTokens: m.baseEnv.MaxTokens, ContextLimit: m.baseEnv.ContextLimit,
			Temperature: m.baseEnv.Temperature,
		}
		screen, err := workflowui.NewScreen(m.base, cfg, dialogs, m.workflowSurface)
		if err != nil {
			m.openDialog("Workflows", []string{err.Error()})
			return
		}
		m.workflows = screen
	}
	m.workflows.Activate()
	m.inspector = toolInspector{}
	m.fitBottom()
}
func (m *model) workflowCommand(action string) tea.Cmd {
	if !m.workflowActive() {
		m.openWorkflows()
	}
	if !m.workflowActive() {
		return nil
	}
	return m.workflows.Command(action)
}
func (m *model) workflowScreen() string {
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
func (m *model) workflowDialog(content string) string {
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
