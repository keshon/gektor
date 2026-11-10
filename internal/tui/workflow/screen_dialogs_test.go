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
		f := m.dialog.form
		if !f.Destructive || f.Index != len(f.Fields)+1 || !f.Fields[0].ReadOnly {
			t.Fatal("removal lost context, destructive style or Cancel-first focus")
		}
		m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		if w.editor.Source() != before || m.dialog == nil {
			t.Fatal("save shortcut performed a removal")
		}
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.dialog != nil || w.editor.Source() != before {
			t.Fatal("Enter on Cancel removed a value")
		}
		open()
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.dialog != nil || w.editor.Source() == before {
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
		if m.dialog.form.ApplyLabel != item.label || !strings.Contains(m.dialog.title, "JSON") {
			t.Fatal("JSON form does not name what its primary action does")
		}
		m.closeDialog()
	}
}
