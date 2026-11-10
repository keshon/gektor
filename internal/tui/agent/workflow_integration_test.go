package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/permission"
	workflowui "github.com/keshon/gektor/internal/tui/workflow"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

func workflowTestModel(t *testing.T) *Model {
	t.Helper()
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := sizeModelTo(t, testModel(), 120, 30)
	m.state = stDone
	m.base = context.Background()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	m.baseEnv.WS = ws
	m.wsRoot = ws.Root()
	m.baseEnv.Policy = permission.Default()
	m.newWorkflow = workflowui.NewScreen
	m.openWorkflows()
	if m.workflows == nil {
		t.Fatal("workspace unavailable")
	}
	t.Cleanup(m.workflows.Close)
	return m
}

func workflowRecipe() workflow.Definition {
	return workflow.Definition{Version: 3, Name: "Test", Steps: []workflow.Step{{ID: "transform", Tool: "script_transform", NodeVersion: 1, Args: json.RawMessage(`{"code":"return input + 1;","input":2}`)}}}
}

func TestWorkflowStartupOpensEditorWithoutStartingRun(t *testing.T) {
	m := workflowTestModel(t)
	m.workflows = nil
	m.startWorkflows = true
	draft := "unsent Agent text"
	m.input.SetValue(draft)
	m.Init()
	if !m.workflowActive() || m.workflows.State().Busy || m.workflows.State().InspectingRun || m.state != stDone || m.input.Value() != draft {
		t.Fatal("startup did not open an idle workflow editor")
	}
	// Init should not reopen Workflows after returning to Agent.
	m.workflows.Hide()
	m.Init()
	if m.workflowActive() {
		t.Fatal("startup action repeated")
	}
}

func TestWorkflowMenuCanvasAndModalGeometry(t *testing.T) {
	m := workflowTestModel(t)
	seedWorkflow(t, m, workflowRecipe())
	for _, size := range [][2]int{{120, 30}, {80, 24}, {40, 15}} {
		sizeModelTo(t, m, size[0], size[1])
		v := ansi.Strip(m.screenContent())
		rows := strings.Split(v, "\n")
		if len(rows) != size[1] {
			t.Fatal("height")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != size[0] {
				t.Fatalf("bad row: %q", row)
			}
		}
		if !strings.Contains(v, "transform") {
			t.Fatal(v)
		}
	}
	sizeModelTo(t, m, 120, 30)
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyF9})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.header.menu != "app" {
		t.Fatal("canvas swallowed menu keys")
	}
	m.closeHeader()
	m.workflowCommand("edit")
	left, top, width, height := m.settingsArea()
	if left != 1 || top != 4 || width != 118 || height != 22 {
		t.Fatalf("geometry %d %d %d %d", left, top, width, height)
	}
	if !strings.Contains(ansi.Strip(m.screenContent()), "script_transform") {
		t.Fatal("modal missing")
	}
}

func TestWorkflowOutlineJoins(t *testing.T) {
	m := workflowTestModel(t)
	for _, minimal := range []bool{false, true} {
		m.minimal = minimal
		if !m.workflows.State().InspectorOpen {
			m.workflowCommand("inspector")
		}
		_, _ = m.workflows.CanvasSize()
		rows := strings.Split(ansi.Strip(m.screenContent()), "\n")
		if len(rows) != m.termH {
			t.Fatal("wrong height")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != m.termW {
				t.Fatal("wrong width")
			}
		}
		if minimal {
			if strings.Contains(rows[3], "┬") {
				t.Fatal("outline leaked into minimal mode")
			}
			continue
		}
		at := func(y, x int) rune { return []rune(rows[y])[x] }
		if at(1, 0) != '┌' || at(1, m.termW-1) != '┐' || at(2, 0) != '│' || at(1, m.termW/2) != '┌' || at(3, m.termW/2) != '├' || at(m.termH-2, m.termW/2) != '└' || at(m.termH-3, 0) != '│' || at(m.termH-2, 0) != '└' {
			t.Fatal(strings.Join(rows, "\n"))
		}
	}
}

func TestWorkflowGlobalNavigationStaysWithRoot(t *testing.T) {
	m := workflowTestModel(t)
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyF9})
	if !m.header.focused {
		t.Fatal("workflow swallowed the application menu key")
	}
	m.closeHeader()
	seedWorkflow(t, m, workflowRecipe())
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyF10})
	if m.dialog == nil || m.dialog.title != "Quit with unsaved workflow?" || m.quit {
		t.Fatal("workflow bypassed the root's unsaved-draft protection")
	}
}

func TestWorkflowAndAgentInspectorFramesMatch(t *testing.T) {
	for _, width := range []int{40, 80, 100, 121} {
		for _, minimal := range []bool{false, true} {
			m := workflowTestModel(t)
			sizeModelTo(t, m, width, 30)
			m.minimal = minimal
			seedWorkflow(t, m, workflowRecipe())
			m.workflowCommand("inspector")
			workflow := strings.Split(ansi.Strip(m.workflowScreen()), "\n")
			agent := inspectorModel(t, width)
			agent.minimal = minimal
			agent.openInspector(len(agent.transcript.blocks) - 1)
			chat := strings.Split(ansi.Strip(agent.screenContent()), "\n")
			left := 0
			if width >= inspectorSplitWidth {
				left = width / 2
			}
			for _, y := range []int{1, 3, 26, 28} {
				if ansi.Cut(workflow[y], left, width) != ansi.Cut(chat[y], left, width) {
					t.Fatalf("inspector frame mismatch width %d minimal %v row %d\n%q\n%q", width, minimal, y, workflow[y], chat[y])
				}
			}
			for _, row := range workflow {
				if ansi.StringWidth(row) != width {
					t.Fatal("width overflow", row)
				}
			}
			m.workflows.Update(tea.MouseClickMsg{X: width - 4, Y: 2, Button: tea.MouseLeft})
			if m.workflows.State().InspectorOpen || !m.workflows.State().Active {
				t.Fatal("close left workflow mode")
			}
		}
	}
}

func seedWorkflow(t *testing.T, m *Model, d workflow.Definition) *workflowui.Editor {
	t.Helper()
	if m.workflows != nil {
		m.workflows.Close()
	}
	draft := workflowui.New()
	draft.Replace(d)
	var err error
	m.workflows, err = workflowui.NewScreen(m.base, workflowui.Config{WS: m.baseEnv.WS, Policy: m.baseEnv.Policy, Draft: draft}, m.screenHost(), m.workflowSurface)
	if err != nil {
		t.Fatal(err)
	}
	m.workflows.Activate()
	t.Cleanup(m.workflows.Close)
	return draft
}

func TestWorkflowMenusAndToolbarKeepNodeCreationVisible(t *testing.T) {
	m := workflowTestModel(t)
	m.header.menu = "recipe"
	if len(m.rawHeaderItems()) > 7 {
		t.Fatal("recipe menu remains overloaded")
	}
	m.header.menu = "node"
	found := false
	for _, item := range m.rawHeaderItems() {
		if item.command == "/workflows add" && item.enabled {
			found = true
		}
	}
	if !found {
		t.Fatal("Add node missing from Node menu")
	}
	m.closeHeader()
	for _, width := range []int{40, 120} {
		sizeModelTo(t, m, width, 24)
		found = false
		for _, action := range m.workflows.Actions() {
			if action.Key == 'a' && action.Enabled {
				found = true
			}
		}
		if !found {
			t.Fatal("Add node hidden in toolbar", width)
		}
	}
}
