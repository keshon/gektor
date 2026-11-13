package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/workflow"
)

func TestWorkflowValueRemovalStartsOnCancelAndRequiresExplicitAction(t *testing.T) {
	for _, kind := range []string{"inputs", "outputs"} {
		m := workflowTestModel(t)
		w := m.workflows
		d := workflowRecipe()
		d.Inputs = map[string]workflow.Input{"value": {Type: "string"}}
		d.Outputs = map[string]json.RawMessage{"value": json.RawMessage(`"result"`)}
		w.editor.Replace(d)
		before := w.editor.Source()
		open := func() {
			m.workflows.parameterEdit(workflowParameterEdit{Kind: kind, Name: "value", Draft: before, Remove: true})
		}
		open()
		f := m.chrome.Dialog.Form
		if !f.Destructive || f.Index != len(f.Fields)+1 || !f.Fields[0].ReadOnly {
			t.Fatal("removal lost context, destructive style or Cancel-first focus")
		}
		m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		if w.editor.Source() != before || m.chrome.Dialog == nil {
			t.Fatal("save shortcut performed a removal")
		}
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.chrome.Dialog != nil || w.editor.Source() != before {
			t.Fatal("Enter on Cancel removed a value")
		}
		open()
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.chrome.Dialog != nil || w.editor.Source() == before {
			t.Fatal("explicit removal did not apply")
		}
		w.editor.Undo()
		if w.editor.Source() != before {
			t.Fatal("removal lost undo support")
		}
	}
}

func TestWorkflowJSONInputsNameTheirActualAction(t *testing.T) {
	m := workflowTestModel(t)
	for _, item := range []struct{ action, label string }{{"run", "Run"}, {"validate", "Check"}, {"schedule", "Next"}} {
		m.workflows.inputForm(workflowInvocation{Action: item.action, Mode: "JSON", Draft: m.workflows.editor.Source()})
		if m.chrome.Dialog.Form.ApplyLabel != item.label || !strings.Contains(m.chrome.Dialog.Title, "JSON") {
			t.Fatal("JSON form does not name what its primary action does")
		}
		m.closeDialog()
	}
}

func TestDraftFormsRejectChangedTargets(t *testing.T) {
	for _, action := range []string{"behavior", "recipe", "delete"} {
		t.Run(action, func(t *testing.T) {
			m := workflowTestModel(t)
			w := m.workflows
			w.editor.Replace(workflowRecipe())
			w.graph.Selected = "transform"
			m.workflowCommand(action)
			values := m.chrome.Dialog.Form.Values()
			changed := w.editor.Clone()
			changed.Steps = nil
			w.editor.Replace(changed)
			before := w.editor.Source()

			err := m.applyValues(values)
			if err == nil || !strings.Contains(err.Error(), "draft changed") || w.editor.Source() != before {
				t.Fatal("stale form changed a different draft", err)
			}
		})
	}
}

func TestCollectionBehaviorFormPreservesOperationAndEditsPolicy(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	d.Steps[0].Args = json.RawMessage(`{}`)
	d.Steps[0].ForEach = json.RawMessage(`[{"code":"return input;","input":1}]`)
	d.Steps[0].Concurrency = 2
	d.Steps[0].OnError = "stop"
	w.editor.Replace(d)
	w.graph.Selected = d.Steps[0].ID
	m.workflowCommand("behavior")
	values := m.chrome.Dialog.Form.Values()
	values["concurrency"] = "3"
	values["on_error"] = "collect"
	err := m.applyValues(values)
	if err != nil {
		t.Fatal(err)
	}
	step := w.editor.Definition.Steps[0]
	if step.ID != d.Steps[0].ID || step.Tool != d.Steps[0].Tool || step.Concurrency != 3 || step.OnError != "collect" {
		t.Fatal("behavior edit lost operation or policy", step)
	}
}

func TestRepeatBehaviorFormPreservesAndEditsPolicy(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	d.Steps[0].Tool = "workflow_call"
	d.Steps[0].Args = json.RawMessage(`{"recipe_path":"page.json","inputs":{"cursor":0}}`)
	d.Steps[0].Repeat = &workflow.RepeatPolicy{MaxIterations: 3, Until: "/done", NextInputs: "/next"}
	w.editor.Replace(d)
	w.graph.Selected = d.Steps[0].ID
	m.workflowCommand("behavior")
	values := m.chrome.Dialog.Form.Values()
	values["repeat"] = `{"max_iterations":5,"until":"/finished","next_inputs":"/cursor","delay_ms":100}`

	err := m.applyValues(values)
	if err != nil {
		t.Fatal(err)
	}
	step := w.editor.Definition.Steps[0]
	if step.Tool != "workflow_call" || string(step.Args) != string(d.Steps[0].Args) || step.Repeat == nil || step.Repeat.MaxIterations != 5 || step.Repeat.Until != "/finished" || step.Repeat.NextInputs != "/cursor" || step.Repeat.DelayMS != 100 {
		t.Fatal("behavior edit lost operation or repeat policy", step)
	}
}
