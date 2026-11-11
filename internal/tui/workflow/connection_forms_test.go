package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
)

func nextConnectionForm(t *testing.T, m *model) {
	t.Helper()
	if len(m.workflows.commands) == 0 {
		t.Fatal("no queued form")
	}
	action := m.workflows.commands[len(m.workflows.commands)-1]()
	m.chrome.Dialog = nil
	m.workflows.action(action.(workflowAction))
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil {
		t.Fatal("missing connection form")
	}
}

func TestConnectionFormsCreateEditRemoveAndUndo(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	second := d.Steps[0]
	second.ID = "second"
	d.Steps = append(d.Steps, second)
	w.editor.Replace(d)
	w.graph.Selected = "second"
	before := w.editor.Source()
	m.workflows.linkTarget(`["transform","second"]`)
	if err := m.chrome.Dialog.Form.Apply(map[string]string{"from": "transform", "arg": "Argument: input"}); err != nil {
		t.Fatal(err)
	}
	nextConnectionForm(t, m)
	current := m.chrome.Dialog.Form.Fields[0]
	if !current.ReadOnly || current.Value != "2" {
		t.Fatal("replacement value hidden", current)
	}
	if err := m.chrome.Dialog.Form.Apply(map[string]string{"pointer": "/first"}); err != nil {
		t.Fatal(err)
	}
	created := w.editor.Source()
	m.workflowCommand("connections")
	f := m.chrome.Dialog.Form
	label := f.Choices["connection"][0]
	if !strings.Contains(label, "args/input") || !strings.Contains(label, "/first") {
		t.Fatal(label)
	}
	if err := f.Apply(map[string]string{"connection": label, "action": "Edit"}); err != nil {
		t.Fatal(err)
	}
	nextConnectionForm(t, m)
	if err := m.chrome.Dialog.Form.Apply(map[string]string{"from": "transform", "arg": "Argument: input"}); err != nil {
		t.Fatal(err)
	}
	nextConnectionForm(t, m)
	if err := m.chrome.Dialog.Form.Apply(map[string]string{"pointer": "/second"}); err != nil {
		t.Fatal(err)
	}
	edited := w.editor.Source()
	w.editor.Undo()
	if w.editor.Source() != created {
		t.Fatal("mapping edit not undoable")
	}
	w.editor.Redo()
	m.workflowCommand("connections")
	f = m.chrome.Dialog.Form
	label = f.Choices["connection"][0]
	if err := f.Apply(map[string]string{"connection": label, "action": "Remove"}); err != nil {
		t.Fatal(err)
	}
	nextConnectionForm(t, m)
	if f := m.chrome.Dialog.Form; !f.Destructive || f.Index != len(f.Fields)+1 {
		t.Fatal("connection removal does not start on Cancel")
	}
	if err := m.chrome.Dialog.Form.Apply(nil); err != nil {
		t.Fatal(err)
	}
	if len(w.editor.Connections("second")) != 0 || !strings.Contains(w.notice, "second:") || !strings.Contains(w.notice, "input") {
		t.Fatal("missing required argument not visible", w.notice)
	}
	w.editor.Undo()
	if w.editor.Source() != edited {
		t.Fatal("removal not undoable")
	}
	w.editor.Undo()
	w.editor.Undo()
	if w.editor.Source() != before {
		t.Fatal("initial draft not restored")
	}
}

func TestConnectionFormsProtectFrozenAndStaleDrafts(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Replace(workflowRecipe())
	w.run = &workflow.Run{}
	m.workflowCommand("connections")
	if m.chrome.Dialog != nil {
		t.Fatal("frozen run opened edit form")
	}
	w.run = nil
	edit := workflowConnectionEdit{From: "transform", To: "transform", Field: "args", Path: "/input", Draft: w.editor.Source()}
	d := w.editor.Clone()
	d.Name = "Changed"
	w.editor.Replace(d)
	m.workflows.connectionEdit(edit)
	if m.chrome.Dialog != nil || !strings.Contains(w.notice, "Draft changed") {
		t.Fatal(w.notice)
	}
}

func TestSourceOutputHintsUseEscapedSchemaPointers(t *testing.T) {
	hints := workflowOutputPointers(json.RawMessage(`{"type":"object","properties":{"a/b":{"properties":{"~name":{"type":"string"}}},"items":{"type":"array","items":{"type":"object"}}}}`))
	if strings.Join(hints, ",") != "/a~1b,/a~1b/~0name,/items" {
		t.Fatal(hints)
	}
}
