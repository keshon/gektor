package workflow

import (
	"context"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/workspace"
)

// model is a headless overlay host. Feature tests use real form controls without
// constructing an Agent session, transcript, provider connection, or app shell.
type model struct {
	workflows    *Model
	dialog       *testDialog
	wsRoot       string
	termW, termH int
	minimal      bool
	base         context.Context
	baseEnv      struct {
		WS     *workspace.Workspace
		Policy permission.Policy
	}
}
type testForm struct {
	settings.Model
	apply func(map[string]string) error
}
type testDialog struct {
	title   string
	lines   []string
	form    *testForm
	save    func() error
	onClose func()
}

const paneTop = 4

func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.dialog == nil || m.dialog.form == nil {
		return m.workflows.Update(msg)
	}
	form := m.dialog.form
	intent, cmd := form.UpdateKey(msg, settings.Props{Width: m.termW - 2, Height: m.termH - 8, Title: m.dialog.title})
	switch intent.Action {
	case settings.Cancel:
		m.closeDialog()
	case settings.Apply:
		if err := form.apply(form.Values()); err != nil {
			form.Error = err.Error()
		} else {
			m.closeDialog()
		}
	}
	return m.workflows, cmd
}

func workflowTestModel(t *testing.T) *model {
	t.Helper()
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	m := &model{base: context.Background(), wsRoot: ws.Root(), termW: 120, termH: 30}
	m.baseEnv.WS = ws
	m.baseEnv.Policy = permission.Default()
	host := screen.Host{
		OpenForm: m.openForm, CloseDialog: m.closeDialog,
		OpenDialog: func(d screen.Dialog) {
			m.dialog = &testDialog{title: d.Title, lines: d.Lines, save: d.Save, onClose: d.OnClose}
		},
		FinishOperation: func() {
			if m.dialog != nil && m.dialog.onClose != nil {
				m.closeDialog()
			}
		},
	}
	m.workflows, err = NewScreen(m.base, Config{WS: ws, Policy: m.baseEnv.Policy}, host, m.surface)
	if err != nil {
		t.Fatal(err)
	}
	m.workflows.Activate()
	t.Cleanup(m.workflows.Close)
	return m
}
func (m *model) openForm(f screen.Form) *settings.Model {
	editor := textarea.New()
	editor.ShowLineNumbers = false
	editor.Prompt = ""
	editor.CharLimit = 65536
	editor.SetVirtualCursor(true)
	form := &testForm{Model: settings.NewModel(f.Fields, editor), apply: f.Apply}
	form.Hint, form.ApplyLabel = f.Hint, f.ApplyLabel
	form.Choices, form.FileFields, form.Destructive = f.Choices, f.FileFields, f.Destructive
	index := 0
	if f.StartOnCancel {
		index = len(f.Fields) + 1
	}
	form.Focus(index, form.FieldVisible)
	m.dialog = &testDialog{title: f.Title, form: form, onClose: f.OnClose}
	return &form.Model
}
func (m *model) closeDialog() {
	if m.dialog != nil && m.dialog.onClose != nil {
		m.dialog.onClose()
	}
	m.dialog = nil
}
func (m *model) surface() screen.Surface {
	return screen.Surface{Area: pane.Rect{Y: 1, Width: m.termW, Height: max(m.termH-2, 1)}, Inset: 1, SplitWidth: 100, Outlined: !m.minimal, BottomEdge: true}
}
func (m *model) screenPanes(open bool) pane.SplitLayout { return m.surface().Panes(open) }
func (m *model) panelGeometry(width, footer int, close, conversation bool) pane.Layout {
	return m.surface().Layout(width, close)
}
func (m *model) workflowCommand(action string) tea.Cmd { return m.workflows.Command(action) }
func (m *model) workflowScreen() string {
	return strings.Repeat(" ", m.termW) + "\n" + m.workflows.Content() + "\n" + strings.Repeat(" ", m.termW)
}
func (m *model) screenContent() string {
	if m.dialog == nil {
		return m.workflowScreen()
	}
	if m.dialog.form != nil {
		return m.dialog.form.View(settings.Props{Width: m.termW - 2, Height: m.termH - 8, Title: m.dialog.title})
	}
	return strings.Join(m.dialog.lines, "\n")
}
func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) { return m.workflows.Update(msg) }
func sizeModelTo(t *testing.T, m *model, width, height int) *model {
	t.Helper()
	m.termW, m.termH = width, height
	return m
}
