package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/table"
	"github.com/keshon/gektor/internal/workspace"
)

// workflowUI supplies shared terminal presentation and navigation, without access
// to the parent agent, transcript, session factory or application state.
type workflowUI struct {
	openForm         func(string, string, string, []formField, func(map[string]string) error)
	openRemoval      func(string, string, string, []formField, func(map[string]string) error)
	openDialog       func(string, []string)
	closeDialog      func()
	dialog           func() *infoDialog
	newTable         func() *table.Model
	screenPanes      func(bool) pane.SplitLayout
	panelGeometry    func(int, int, bool, bool) pane.Layout
	inspectionRows   func(string, string, string, int, bool, bool) []string
	inspectionHints  func(int, ...popup.Shortcut) string
	graphStyles      func() graph.Styles
	noticeStyles     func() pane.NoticeStyles
	styles           func() styles
	headerFocused    func() bool
	width, height    func() int
	actionBarVisible func() bool
	toolbarClick     func(int) (tea.Model, tea.Cmd)
	footerKey        func(tea.KeyPressMsg) (tea.Model, tea.Cmd)
	requestQuit      func() (tea.Model, tea.Cmd)
}
type workflowScreenConfig struct {
	WS                      *workspace.Workspace
	Client                  llm.Client
	Policy                  permission.Policy
	MaxTokens, ContextLimit int
	Temperature             float64
}

func (m *model) workflowActive() bool { return m.workflows != nil && m.workflows.active }
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
		ui := workflowUI{openForm: m.openForm, openRemoval: m.openRemoval, openDialog: m.openDialog, closeDialog: m.closeDialog,
			dialog: func() *infoDialog { return m.dialog }, newTable: m.newTable, screenPanes: m.screenPanes, panelGeometry: m.panelGeometry,
			inspectionRows: m.inspectionRows, inspectionHints: m.inspectionHints, graphStyles: m.graphStyles, noticeStyles: m.noticeStyles,
			styles: func() styles { return m.styles }, headerFocused: func() bool { return m.header.focused }, width: func() int { return m.termW }, height: func() int { return m.termH },
			actionBarVisible: m.actionBarVisible, toolbarClick: m.toolbarClick, footerKey: m.footerKey, requestQuit: m.requestQuit}
		cfg := workflowScreenConfig{WS: m.baseEnv.WS, Client: m.baseEnv.Client, Policy: m.baseEnv.Policy, MaxTokens: m.baseEnv.MaxTokens, ContextLimit: m.baseEnv.ContextLimit, Temperature: m.baseEnv.Temperature}
		screen, err := newWorkflowScreen(m.base, cfg, ui)
		if err != nil {
			m.openDialog("Workflows", []string{err.Error()})
			return
		}
		m.workflows = screen
	}
	m.workflows.active = true
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
	return m.workflows.command(action)
}
func (m *model) workflowScreen() string {
	content := cellLine(m.menuLine(), m.termW) + "\n" + m.workflows.content() + "\n" + m.actionBar()
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
