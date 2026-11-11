package workflow

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/workspace"
)

// model hosts one real Workflow screen and its production shared controls.
// Application navigation remains covered by tests in the parent package.
type model struct {
	workflows    *Model
	chrome       *screen.Controls
	wsRoot       string
	termW, termH int
	minimal      bool
	base         context.Context
	baseEnv      struct {
		WS     *workspace.Workspace
		Policy permission.Policy
	}
}

const paneTop = 4

func workflowTestModel(t *testing.T) *model {
	t.Helper()
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	m := &model{base: t.Context(), chrome: &screen.Controls{}, wsRoot: ws.Root(), termW: 120, termH: 30}
	m.baseEnv.WS, m.baseEnv.Policy = ws, permission.Default()
	m.workflows, err = New(m.base, Config{WS: ws, Policy: m.baseEnv.Policy}, m.chrome.Host(), m.surface)
	if err != nil {
		t.Fatal(err)
	}
	m.workflows.Activate()
	m.chrome.Changed = m.syncControls
	m.syncControls()
	t.Cleanup(m.workflows.Close)
	return m
}
func (m *model) syncControls() {
	props := m.workflows.Presentation()
	props.Width, props.Height = m.termW, m.termH
	m.chrome.Props = props
	if m.chrome.Dialog != nil {
		m.chrome.Props.Actions = m.chrome.DialogActions()
	}
}
func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.syncControls()
	if m.chrome.Dialog != nil {
		if m.chrome.Dialog.Form != nil {
			return m.workflows, m.chrome.FormKey(msg)
		}
		if m.chrome.Dialog.Table != nil {
			return m.workflows, m.chrome.TableInput(msg)
		}
		return m.workflows, m.chrome.InfoKey(msg)
	}
	return m.workflows.Update(msg)
}
func (m *model) closeDialog() { m.chrome.Close() }
func (m *model) surface() screen.Surface {
	return screen.Surface{Area: pane.Rect{Y: 1, Width: m.termW, Height: max(m.termH-2, 1)}, Inset: 1, SplitWidth: 100, Outlined: !m.minimal, BottomEdge: true}
}
func (m *model) screenPanes(open bool) pane.SplitLayout { return m.surface().Panes(open) }
func (m *model) panelGeometry(width, footer int, close, conversation bool) pane.Layout {
	return m.surface().Layout(width, close)
}
func (m *model) workflowCommand(action string) tea.Cmd {
	m.syncControls()
	return m.workflows.Command(action)
}
func (m *model) workflowScreen() string {
	m.syncControls()
	return m.chrome.Compose(m.workflows.View()).Content
}
func (m *model) screenContent() string { return m.workflowScreen() }
func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.syncControls()
	if m.chrome.Dialog != nil {
		return m.workflows, m.chrome.DialogMouse(msg)
	}
	return m.workflows.Update(msg)
}
func sizeModelTo(t *testing.T, m *model, width, height int) *model {
	t.Helper()
	m.termW, m.termH = width, height
	m.syncControls()
	return m
}
