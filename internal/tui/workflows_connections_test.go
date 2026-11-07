package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/automation"
)

func nextConnectionForm(t *testing.T, m *model) {
	t.Helper()
	if len(m.pendingCommands) == 0 {
		t.Fatal("no queued form")
	}
	action := m.pendingCommands[len(m.pendingCommands)-1]()
	m.dialog = nil
	m.workflowAction(action.(workflowAction))
	if m.dialog == nil || m.dialog.form == nil {
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
	m.workflowLinkTarget(`["transform","second"]`)
	if err := m.dialog.form.apply(map[string]string{"from": "transform", "arg": "Argument: input"}); err != nil {
		t.Fatal(err)
	}
	nextConnectionForm(t, m)
	current := m.dialog.form.Fields[0]
	if !current.ReadOnly || current.Value != "2" {
		t.Fatal("replacement value hidden", current)
	}
	if err := m.dialog.form.apply(map[string]string{"pointer": "/first"}); err != nil {
		t.Fatal(err)
	}
	created := w.editor.Source()
	m.workflowCommand("connections")
	f := m.dialog.form
	label := f.Choices["connection"][0]
	if !strings.Contains(label, "args/input") || !strings.Contains(label, "/first") {
		t.Fatal(label)
	}
	if err := f.apply(map[string]string{"connection": label, "action": "Edit"}); err != nil {
		t.Fatal(err)
	}
	nextConnectionForm(t, m)
	if err := m.dialog.form.apply(map[string]string{"from": "transform", "arg": "Argument: input"}); err != nil {
		t.Fatal(err)
	}
	nextConnectionForm(t, m)
	if err := m.dialog.form.apply(map[string]string{"pointer": "/second"}); err != nil {
		t.Fatal(err)
	}
	edited := w.editor.Source()
	w.editor.Undo()
	if w.editor.Source() != created {
		t.Fatal("mapping edit not undoable")
	}
	w.editor.Redo()
	m.workflowCommand("connections")
	f = m.dialog.form
	label = f.Choices["connection"][0]
	if err := f.apply(map[string]string{"connection": label, "action": "Remove"}); err != nil {
		t.Fatal(err)
	}
	nextConnectionForm(t, m)
	if f := m.dialog.form; !f.Destructive || f.Index != len(f.Fields)+1 {
		t.Fatal("connection removal does not start on Cancel")
	}
	if err := m.dialog.form.apply(nil); err != nil {
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
	w.run = &automation.Run{}
	m.workflowCommand("connections")
	if m.dialog != nil {
		t.Fatal("frozen run opened edit form")
	}
	w.run = nil
	edit := workflowConnectionEdit{From: "transform", To: "transform", Field: "args", Path: "/input", Draft: w.editor.Source()}
	d := w.editor.Clone()
	d.Name = "Changed"
	w.editor.Replace(d)
	m.workflowConnectionEdit(edit)
	if m.dialog != nil || !strings.Contains(w.notice, "Draft changed") {
		t.Fatal(w.notice)
	}
}

func TestSourceOutputHintsUseEscapedSchemaPointers(t *testing.T) {
	hints := workflowOutputPointers(json.RawMessage(`{"type":"object","properties":{"a/b":{"properties":{"~name":{"type":"string"}}},"items":{"type":"array","items":{"type":"object"}}}}`))
	if strings.Join(hints, ",") != "/a~1b,/a~1b/~0name,/items" {
		t.Fatal(hints)
	}
}

func TestConnectionPreviewFitsResizedForms(t *testing.T) {
	previous := selectedPalette.Load()
	defer selectedPalette.Store(previous)
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	second := d.Steps[0]
	second.ID = "second"
	d.Steps = append(d.Steps, second)
	w.editor.Replace(d)
	for _, theme := range []string{"light", "dark"} {
		setTheme(theme)
		m.workflowConnectionEdit(workflowConnectionEdit{From: "transform", To: "second", Field: "args", Path: "/input", Draft: w.editor.Source()})
		for _, size := range [][2]int{{120, 30}, {80, 24}, {40, 15}} {
			sizeModelTo(t, m, size[0], size[1])
			rows := strings.Split(ansi.Strip(m.screenContent()), "\n")
			if len(rows) != size[1] {
				t.Fatal("form overflow height", theme, size)
			}
			for _, row := range rows {
				if ansi.StringWidth(row) != size[0] {
					t.Fatal("form overflow width", theme, size, row)
				}
			}
		}
	}
}
