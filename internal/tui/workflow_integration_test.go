package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	workflowui "github.com/keshon/gektor/internal/tui/workflow"
	"github.com/keshon/gektor/internal/workflow"
)

func workflowTestModel(t *testing.T) *Model {
	t.Helper()
	m := shellTestModel(t, Config{})
	m.openWorkflows()
	m.syncPresentation()
	if m.workflows == nil {
		t.Fatal("workspace unavailable")
	}
	return m
}

func workflowRecipe() workflow.Definition {
	return workflow.Definition{Version: 4, Name: "Test", Steps: []workflow.Step{{ID: "transform", Tool: "script_transform", NodeVersion: 1, Args: json.RawMessage(`{"code":"return input + 1;","input":2}`)}}}
}

func TestWorkflowStartupOpensEditorWithoutStartingRun(t *testing.T) {
	draft := "unsent Agent text"
	m := shellTestModel(t, Config{StartScreen: "workflows", AgentConfig: AgentConfig{InitialDraft: draft}})
	m.Init()
	if !m.workflowActive() || m.workflows.State().Busy || m.workflows.State().InspectingRun || !m.agent.Status().Idle {
		t.Fatal("startup did not open an idle workflow editor")
	}
	m.showAgent()
	m.Init()
	if m.workflowActive() || !strings.Contains(ansi.Strip(m.View().Content), draft) {
		t.Fatal("startup action repeated or Agent draft lost")
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
	if m.chrome.Header.Menu != "app" {
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
		setMinimal(t, m, minimal)
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
	if !m.chrome.Header.Focused {
		t.Fatal("workflow swallowed the application menu key")
	}
	m.closeHeader()
	seedWorkflow(t, m, workflowRecipe())
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyF10})
	if m.chrome.Dialog == nil || m.chrome.Dialog.Title != "Quit with unsaved workflow?" || m.quit {
		t.Fatal("workflow bypassed the root's unsaved-draft protection")
	}
}

func TestWorkflowAndAgentInspectorFramesMatch(t *testing.T) {
	for _, width := range []int{40, 80, 100, 121} {
		for _, minimal := range []bool{false, true} {
			m := workflowTestModel(t)
			sizeModelTo(t, m, width, 30)
			setMinimal(t, m, minimal)
			seedWorkflow(t, m, workflowRecipe())
			m.workflowCommand("inspector")
			workflow := strings.Split(ansi.Strip(m.workflowScreen()), "\n")
			agent := shellInspectorModel(t, width)
			setMinimal(t, agent, minimal)
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
	draft := workflowui.NewEditor()
	draft.Replace(d)
	var err error
	m.workflows, err = workflowui.New(m.base, workflowui.Config{WS: m.launch.Env.WS, Policy: m.launch.Env.Policy, Draft: draft}, m.chrome, m.workflowSurface())
	if err != nil {
		t.Fatal(err)
	}
	m.workflows.Activate()
	m.active = workflowScreen
	m.syncPresentation()
	t.Cleanup(m.workflows.Close)
	return draft
}

func TestWorkflowMenusAndToolbarKeepNodeCreationVisible(t *testing.T) {
	m := workflowTestModel(t)
	m.chrome.Header.Menu = "recipe"
	if len(m.rawHeaderItems()) > 7 {
		t.Fatal("recipe menu remains overloaded")
	}
	m.chrome.Header.Menu = "node"
	found := false
	for _, item := range m.rawHeaderItems() {
		if item.Command == "/workflows add" && item.Enabled {
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
		for _, action := range m.workflows.Presentation().Actions {
			if action.Key == 'a' && action.Enabled {
				found = true
			}
		}
		if !found {
			t.Fatal("Add node hidden in toolbar", width)
		}
	}
}
