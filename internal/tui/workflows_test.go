package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workspace"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func workflowTestModel(t *testing.T) *model {
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
	m.openWorkflows()
	if m.workflows == nil {
		t.Fatal("workspace unavailable")
	}
	return m
}
func workflowRecipe() automation.Definition {
	return automation.Definition{Version: 3, Name: "Test", Steps: []automation.Step{{ID: "transform", Tool: "script_transform", NodeVersion: 1, Args: json.RawMessage(`{"code":"return input + 1;","input":2}`)}}}
}

func TestWorkflowStartupOpensEditorWithoutStartingRun(t *testing.T) {
	m := workflowTestModel(t)
	m.workflows = nil
	m.startWorkflows = true
	draft := "unsent Agent text"
	m.input.SetValue(draft)
	m.Init()
	if !m.workflowActive() || m.workflows.busy || m.workflows.run != nil || m.state != stDone || m.input.Value() != draft {
		t.Fatal("startup did not open an idle workflow editor")
	}
	// Init should not reopen Workflows after returning to Agent.
	m.workflows.active = false
	m.Init()
	if m.workflowActive() {
		t.Fatal("startup action repeated")
	}
}
func TestWorkflowMenuCanvasAndModalGeometry(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Definition = workflowRecipe()
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
func TestWorkflowExecutionReviewAndSave(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Definition = workflowRecipe()
	w.editor.Definition.Steps[0].Review = true
	w.editor.Path = "recipe.json"
	drain := func(cmd tea.Cmd) workflowResult {
		t.Helper()
		if cmd == nil {
			t.Fatal("no operation")
		}
		msg := cmd()
		for {
			switch v := msg.(type) {
			case workflowRunID:
				w.runID = string(v)
			case workflowPermission:
				t.Fatal("unexpected permission request")
			case workflowResult:
				return v
			default:
				t.Fatalf("unexpected %T", v)
			}
			select {
			case msg = <-w.events:
			case <-time.After(5 * time.Second):
				t.Fatal("operation hung")
			}
		}
	}
	result := drain(m.workflowAction(workflowAction{"save-path", "recipe.json"}))
	if result.err != nil {
		t.Fatal(result.err)
	}
	if cmd := m.acceptWorkflowResult(result); cmd != nil {
		m.acceptWorkflowResult(cmd().(workflowResult))
	}
	if w.editor.Dirty() {
		t.Fatal("save remained dirty")
	}
	if _, err := os.Stat(filepath.Join(m.wsRoot, "recipe.json")); err != nil {
		t.Fatal(err)
	}
	result = drain(m.workflowAction(workflowAction{"run-inputs", "{}"}))
	if result.err != nil {
		t.Fatal(result.err)
	}
	if !strings.Contains(result.text, "waiting_review") {
		t.Fatal(result.text)
	}
	m.acceptWorkflowResult(result)
	result = drain(m.workflowAction(workflowAction{"review-approve", "transform"}))
	if result.err != nil {
		t.Fatal(result.err)
	}
	if !strings.Contains(result.text, "completed") {
		t.Fatal(result.text)
	}
}
func TestWorkflowPermissionDismissalDenies(t *testing.T) {
	m := workflowTestModel(t)
	reply := make(chan permission.Effect, 1)
	m.acceptWorkflowPermission(workflowPermission{tool: "run_program", reply: reply})
	m.closeDialog()
	select {
	case eff := <-reply:
		if eff != permission.Deny {
			t.Fatal(eff)
		}
	default:
		t.Fatal("dismissal left permission waiting")
	}
}
func TestWorkflowFrozenRunCannotEditAndDraftSurvives(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Definition = workflowRecipe()
	source := w.editor.Source()
	w.run = &automation.Run{RunID: "run", Definition: workflowRecipe()}
	m.workflowCommand("add")
	if m.dialog != nil {
		t.Fatal("frozen run editable")
	}
	m.workflowCommand("draft")
	if w.run != nil || w.editor.Source() != source {
		t.Fatal("draft lost")
	}
}

func TestWorkflowNestedPermissionDenialAndCancellation(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Definition = workflowRecipe()
	w.editor.Path = "recipe.json"
	m.baseEnv.Policy = permission.Policy{Rules: []permission.Rule{{Tool: "workflow_run", Pattern: "*", Effect: permission.Allow}, {Tool: "script_transform", Pattern: "*", Effect: permission.Ask}}}
	cmd := m.workflowAction(workflowAction{"run-inputs", "{}"})
	msg := cmd()
	seen := false
	for {
		switch v := msg.(type) {
		case workflowRunID:
			w.runID = string(v)
		case workflowPermission:
			seen = true
			v.reply <- permission.Deny
		case workflowResult:
			if !seen || v.err == nil {
				t.Fatalf("permission bypass: %+v", v)
			}
			return
		default:
			t.Fatalf("unexpected %T", v)
		}
		select {
		case msg = <-w.events:
		case <-time.After(5 * time.Second):
			t.Fatal("hung")
		}
	}
}
func TestWorkflowAIProposalCannotApplyToChangedDraft(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Definition = workflowRecipe()
	before := w.editor.Source()
	m.acceptWorkflowProposal(workflowProposal{before: before, source: before})
	if m.dialog == nil || m.dialog.save == nil {
		t.Fatal("missing proposal review")
	}
	w.editor.Definition.Name = "Changed elsewhere"
	if err := m.dialog.save(); err == nil {
		t.Fatal("stale proposal applied")
	}
}

func TestWorkflowProposalShowsScheduleHandoff(t *testing.T) {
	m := workflowTestModel(t)
	m.workflows.editor.Replace(workflowRecipe())
	before := m.workflows.editor.Source()
	note := "Add a Schedule card to save the trigger. This proposal has not scheduled anything."
	m.acceptWorkflowProposal(workflowProposal{before: before, source: before, note: note})
	if m.dialog == nil || !strings.Contains(strings.Join(m.dialog.lines, "\n"), note) {
		t.Fatal("missing handoff")
	}
}

func TestWorkflowOutlineJoins(t *testing.T) {
	m := workflowTestModel(t)
	for _, minimal := range []bool{false, true} {
		m.minimal = minimal
		m.workflows.inspectorOpen = true
		_, _ = m.workflowCanvasSize()
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
func TestMissingWorkflowKeepsDraftAndOffersPathCorrection(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Definition = workflowRecipe()
	before := w.editor.Source()
	cmd := m.workflowAction(workflowAction{"open-path", "missing.json"})
	result, ok := cmd().(workflowResult)
	if !ok || result.err == nil {
		t.Fatalf("expected missing recipe: %+v", result)
	}
	m.acceptWorkflowResult(result)
	if w.editor.Source() != before {
		t.Fatal("failed open replaced draft")
	}
	if m.dialog == nil || m.dialog.form == nil {
		t.Fatal("path correction form missing")
	}
	if m.dialog.form.Editor.Value() != "missing.json" || !strings.Contains(m.dialog.form.Error, "Recipe not found") {
		t.Fatal(m.dialog.form.Error)
	}
	if !strings.Contains(m.dialog.form.Hint, m.wsRoot) {
		t.Fatal("workspace not shown")
	}
}

func TestWorkflowConnectionUsesDestinationSchema(t *testing.T) {
	m := workflowTestModel(t)
	d := workflowRecipe()
	second := d.Steps[0]
	second.ID = "second"
	d.Steps = append(d.Steps, second)
	m.workflows.editor.Replace(d)
	m.workflowLinkTarget(`["transform","second"]`)
	if m.dialog == nil || m.dialog.form == nil {
		t.Fatal("missing mapping form")
	}
	f := m.dialog.form
	if !strings.Contains(strings.Join(f.Choices["arg"], ","), "Argument: input") {
		t.Fatal(f.Choices)
	}
	before := m.workflows.editor.Source()
	if err := f.apply(map[string]string{"arg": "Argument: nonexistent"}); err == nil {
		t.Fatal("accepted unknown argument")
	}
	if m.workflows.editor.Source() != before {
		t.Fatal("invalid mapping changed draft")
	}
	if err := f.apply(map[string]string{"from": "transform", "arg": "Argument: input"}); err != nil {
		t.Fatal(err)
	}
	m.dialog = nil
	action := m.pendingCommands[len(m.pendingCommands)-1]()
	m.workflowAction(action.(workflowAction))
	f = m.dialog.form
	if err := f.apply(map[string]string{"pointer": "/result"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(m.workflows.editor.Definition.Steps[1].Args), `"$step":"transform"`) {
		t.Fatal("missing data reference")
	}
	m.workflows.editor.Undo()
	if m.workflows.editor.Source() != before {
		t.Fatal("mapping not undoable")
	}
}
