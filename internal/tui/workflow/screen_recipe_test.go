package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
)

func TestRecipeSetupFormsPreserveDataAndUndo(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Replace(workflowRecipe())
	initial := w.editor.Source()
	m.workflowCommand("name")
	if err := m.dialog.form.apply(map[string]string{"name": "Report"}); err != nil {
		t.Fatal(err)
	}
	if w.editor.Definition.Name != "Report" {
		t.Fatal("name not changed")
	}
	afterName := w.editor.Source()
	m.workflowCommand("inputs")
	if err := m.dialog.form.apply(map[string]string{"entry": "Add new", "action": "Edit"}); err != nil {
		t.Fatal(err)
	}
	nextConnectionForm(t, m)
	if err := m.dialog.form.apply(map[string]string{"name": "count", "type": "integer", "required": "true", "default": "9007199254740993"}); err != nil {
		t.Fatal(err)
	}
	input := w.editor.Definition.Inputs["count"]
	if input.Type != "integer" || !input.Required || string(input.Default) != "9007199254740993" {
		t.Fatal(input)
	}
	afterInput := w.editor.Source()
	m.workflowCommand("outputs")
	if err := m.dialog.form.apply(map[string]string{"entry": "Add new", "action": "Edit"}); err != nil {
		t.Fatal(err)
	}
	nextConnectionForm(t, m)
	if err := m.dialog.form.apply(map[string]string{"name": "result", "value": `{"$step":"transform"}`}); err != nil {
		t.Fatal(err)
	}
	if string(w.editor.Definition.Outputs["result"]) != `{"$step":"transform"}` {
		t.Fatal(w.editor.Definition.Outputs)
	}
	w.editor.Undo()
	if w.editor.Source() != afterInput {
		t.Fatal("output undo changed inputs")
	}
	w.editor.Undo()
	if w.editor.Source() != afterName {
		t.Fatal("input undo did not remove declaration")
	}
	w.editor.Undo()
	if w.editor.Source() != initial {
		t.Fatal("name undo changed steps")
	}
	w.editor.Redo()
	w.editor.Redo()
	w.editor.Redo()
	m.workflowCommand("inputs")
	if err := m.dialog.form.apply(map[string]string{"entry": "Named: count", "action": "Remove"}); err != nil {
		t.Fatal(err)
	}
	nextConnectionForm(t, m)
	beforeRemoval := w.editor.Source()
	if err := m.dialog.form.apply(nil); err != nil {
		t.Fatal(err)
	}
	if len(w.editor.Definition.Inputs) != 0 || len(w.editor.Definition.Outputs) != 1 {
		t.Fatal("removal lost unrelated data")
	}
	w.editor.Undo()
	if w.editor.Source() != beforeRemoval {
		t.Fatal("removal undo changed recipe")
	}
}

func TestRecipeSetupRejectsInvalidValuesAndStaleForms(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Replace(workflowRecipe())
	before := w.editor.Source()
	m.workflows.parameterEdit(workflowParameterEdit{Kind: "inputs", Draft: before})
	f := m.dialog.form
	for _, v := range []map[string]string{
		{"name": "wrong", "type": "integer", "default": "1.5"},
		{"name": "wrong", "type": "boolean", "default": "null"},
		{"name": "wrong", "type": "unsupported"},
		{"name": "wrong", "type": "object", "default": `{"$input":"inject"}`},
	} {
		if err := f.apply(v); err == nil {
			t.Fatal("accepted invalid input", v)
		}
		if w.editor.Source() != before {
			t.Fatal("failed form mutated recipe")
		}
	}
	m.workflows.parameterEdit(workflowParameterEdit{Kind: "outputs", Draft: before})
	if err := m.dialog.form.apply(map[string]string{"name": "out", "value": "{} {}"}); err == nil {
		t.Fatal("accepted trailing JSON")
	}
	m.workflowCommand("name")
	f = m.dialog.form
	d := w.editor.Clone()
	d.Name = "Newer"
	w.editor.Replace(d)
	if err := f.apply(map[string]string{"name": "Old"}); err == nil || !strings.Contains(err.Error(), "draft changed") {
		t.Fatal("stale overwrite", err)
	}
	w.run = &workflow.Run{}
	m.dialog = nil
	for _, action := range []string{"name", "inputs", "outputs"} {
		m.workflowCommand(action)
		if m.dialog != nil {
			t.Fatal("frozen run opened setup", action)
		}
	}
}

func TestRecipeSetupEditsCanClearDefaults(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	d.Inputs = map[string]workflow.Input{"text": {Type: "string", Default: json.RawMessage(`"old"`)}}
	w.editor.Replace(d)
	m.workflows.parameterEdit(workflowParameterEdit{Kind: "inputs", Name: "text", Draft: w.editor.Source()})
	if err := m.dialog.form.apply(map[string]string{"type": "string", "required": "false", "default": ""}); err != nil {
		t.Fatal(err)
	}
	if w.editor.Definition.Inputs["text"].Default != nil {
		t.Fatal("cleared default retained")
	}
}
