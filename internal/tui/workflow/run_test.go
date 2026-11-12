package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workflow"
)

func workflowRecipe() workflow.Definition {
	return workflow.Definition{Version: 3, Name: "Test", Steps: []workflow.Step{{ID: "transform", Tool: "script_transform", NodeVersion: 1, Args: json.RawMessage(`{"code":"return input + 1;","input":2}`)}}}
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
	result := drain(m.workflows.action(workflowAction{"save-path", "recipe.json"}))
	if result.err != nil {
		t.Fatal(result.err)
	}
	if cmd := m.workflows.acceptResult(result); cmd != nil {
		m.workflows.acceptResult(cmd().(workflowResult))
	}
	if w.editor.Dirty() {
		t.Fatal("save remained dirty")
	}
	if _, err := os.Stat(filepath.Join(m.wsRoot, "recipe.json")); err != nil {
		t.Fatal(err)
	}
	result = drain(m.workflows.action(workflowAction{"run-inputs", "{}"}))
	if result.err != nil {
		t.Fatal(result.err)
	}
	if !strings.Contains(result.text, "waiting_review") {
		t.Fatal(result.text)
	}
	m.workflows.acceptResult(result)
	result = drain(m.workflows.action(workflowAction{"review-approve", "transform"}))
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
	m.workflows.acceptPermission(workflowPermission{tool: "run_program", reply: reply})
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
	w.run = &workflow.Run{RunID: "run", Definition: workflowRecipe()}
	m.workflowCommand("add")
	if m.chrome.Dialog != nil {
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
	m.workflows.config.Policy = permission.Policy{Rules: []permission.Rule{{Tool: "workflow_run", Pattern: "*", Effect: permission.Allow}, {Tool: "script_transform", Pattern: "*", Effect: permission.Ask}}}
	cmd := m.workflows.action(workflowAction{"run-inputs", "{}"})
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
	m.workflows.acceptProposal(workflowProposal{before: before, source: before})
	if m.chrome.Dialog == nil || !m.chrome.Dialog.Save {
		t.Fatal("missing proposal review")
	}
	w.editor.Definition.Name = "Changed elsewhere"
	if err := m.workflows.applyProposal(); err == nil {
		t.Fatal("stale proposal applied")
	}
}

func TestWorkflowProposalShowsScheduleHandoff(t *testing.T) {
	m := workflowTestModel(t)
	m.workflows.editor.Replace(workflowRecipe())
	before := m.workflows.editor.Source()
	note := "Add a Schedule card to save the trigger. This proposal has not scheduled anything."
	m.workflows.acceptProposal(workflowProposal{before: before, source: before, note: note})
	if m.chrome.Dialog == nil || !strings.Contains(strings.Join(m.chrome.Dialog.Lines, "\n"), note) {
		t.Fatal("missing handoff")
	}
}

func TestMissingWorkflowKeepsDraftAndOffersPathCorrection(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Definition = workflowRecipe()
	before := w.editor.Source()
	cmd := m.workflows.action(workflowAction{"open-path", "missing.json"})
	result, ok := cmd().(workflowResult)
	if !ok || result.err == nil {
		t.Fatalf("expected missing recipe: %+v", result)
	}
	m.workflows.acceptResult(result)
	if w.editor.Source() != before {
		t.Fatal("failed open replaced draft")
	}
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil {
		t.Fatal("path correction form missing")
	}
	if m.chrome.Dialog.Form.Editor.Value() != "missing.json" || !strings.Contains(m.chrome.Dialog.Form.Error, "Recipe not found") {
		t.Fatal(m.chrome.Dialog.Form.Error)
	}
	if !strings.Contains(m.chrome.Dialog.Form.Hint, m.wsRoot) {
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
	m.workflows.linkTarget(`["transform","second"]`)
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil {
		t.Fatal("missing mapping form")
	}
	f := m.chrome.Dialog.Form
	if !strings.Contains(strings.Join(f.Choices["arg"], ","), "Argument: input") {
		t.Fatal(f.Choices)
	}
	before := m.workflows.editor.Source()
	if err := m.applyValues(map[string]string{"arg": "Argument: nonexistent"}); err == nil {
		t.Fatal("accepted unknown argument")
	}
	if m.workflows.editor.Source() != before {
		t.Fatal("invalid mapping changed draft")
	}
	if err := m.applyValues(map[string]string{"from": "transform", "arg": "Argument: input"}); err != nil {
		t.Fatal(err)
	}
	m.chrome.Dialog = nil
	action := m.workflows.commands[len(m.workflows.commands)-1]()
	m.workflows.action(action.(workflowAction))
	f = m.chrome.Dialog.Form
	if err := m.applyValues(map[string]string{"pointer": "/result"}); err != nil {
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

func TestWorkflowReplacementDeniesOnlyTheReplacedPermission(t *testing.T) {
	m := workflowTestModel(t)
	first := make(chan permission.Effect, 1)
	second := make(chan permission.Effect, 1)
	m.workflows.acceptPermission(workflowPermission{tool: "first", reply: first})
	m.workflows.acceptPermission(workflowPermission{tool: "second", reply: second})
	m.syncControls()
	select {
	case effect := <-first:
		if effect != permission.Deny {
			t.Fatal(effect)
		}
	default:
		t.Fatal("replacement lost the old reply")
	}
	select {
	case <-second:
		t.Fatal("replacement answered the new request")
	default:
	}

	err := m.applyValues(map[string]string{"decision": "Allow once"})
	if err != nil {
		t.Fatal(err)
	}
	m.closeDialog()
	select {
	case effect := <-second:
		if effect != permission.Allow {
			t.Fatal(effect)
		}
	default:
		t.Fatal("approval lost its reply")
	}
	select {
	case <-second:
		t.Fatal("closing an answered request sent another reply")
	default:
	}
}

func TestWorkflowShutdownDeniesPendingPermission(t *testing.T) {
	m := workflowTestModel(t)
	reply := make(chan permission.Effect, 1)
	m.workflows.acceptPermission(workflowPermission{tool: "run_program", reply: reply})
	m.workflows.Close()
	select {
	case effect := <-reply:
		if effect != permission.Deny {
			t.Fatal(effect)
		}
	default:
		t.Fatal("shutdown left permission pending")
	}
}

func TestWorkflowSessionApprovalAndAccessReset(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	reply := make(chan permission.Effect, 1)
	w.acceptPermission(workflowPermission{tool: "ssh_exec", resource: "ssh://root@host:22 df -h", reply: reply})
	m.syncControls()
	err := m.applyValues(map[string]string{"decision": "Allow for this session"})
	if err != nil {
		t.Fatal(err)
	}
	m.closeDialog()
	if <-reply != permission.Allow || !w.approvals.Allows("ssh_exec", "ssh://root@host:22 ls /opt") {
		t.Fatal("session approval did not cover the same target")
	}

	w.command("access-full")
	if !w.approvals.FullAccess() || !strings.Contains(m.workflowScreen(), "FULL ACCESS") {
		t.Fatal("full access missing or invisible")
	}
	w.busy = true
	w.command("access-ask")
	if !w.approvals.FullAccess() {
		t.Fatal("access changed during operation")
	}
	w.busy = false
	w.command("access-ask")
	if w.approvals.FullAccess() || w.approvals.Count() != 0 {
		t.Fatal("Ask kept grants")
	}
	w.command("access-full")
	w.action(workflowAction{"new-path", "new.json"})
	if w.approvals.FullAccess() {
		t.Fatal("new recipe inherited full access")
	}
}
