package workflow

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/screen"
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
		f := m.formSnapshot(t)
		if !f.Destructive || f.Index != len(f.Fields)+1 || !f.Fields[0].ReadOnly {
			t.Fatal("removal lost context, destructive style or Cancel-first focus")
		}
		m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		if w.editor.Source() != before || m.chrome.DialogID() == (screen.DialogID{}) {
			t.Fatal("save shortcut performed a removal")
		}
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.chrome.DialogID() != (screen.DialogID{}) || w.editor.Source() != before {
			t.Fatal("Enter on Cancel removed a value")
		}
		open()
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.chrome.DialogID() != (screen.DialogID{}) || w.editor.Source() == before {
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
		if m.formSnapshot(t).ApplyLabel != item.label || !strings.Contains(m.chrome.DialogInfo().Title, "JSON") {
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
			values := m.formSnapshot(t).Values()
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
	w.editBehavior(behaviorEdit{StepID: w.graph.Selected, Section: "Map", Draft: w.editor.Source()})
	values := m.formSnapshot(t).Values()
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
	w.editBehavior(behaviorEdit{StepID: w.graph.Selected, Section: "Loop", Draft: w.editor.Source()})
	values := m.formSnapshot(t).Values()
	values["max_iterations"] = "5"
	values["until"] = "/finished"
	values["next_inputs"] = "/cursor"
	values["delay_ms"] = "100"

	err := m.applyValues(values)
	if err != nil {
		t.Fatal(err)
	}
	step := w.editor.Definition.Steps[0]
	if step.Tool != "workflow_call" || string(step.Args) != string(d.Steps[0].Args) || step.Repeat == nil || step.Repeat.MaxIterations != 5 || step.Repeat.Until != "/finished" || step.Repeat.NextInputs != "/cursor" || step.Repeat.DelayMS != 100 {
		t.Fatal("behavior edit lost operation or repeat policy", step)
	}
}

func TestBehaviorSectionOpensAfterChooserCloses(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Replace(workflowRecipe())
	w.graph.Selected = "transform"
	before := w.editor.Source()
	m.workflowCommand("behavior")
	chooser := m.chrome.DialogID()
	w.HandleControl(screen.Event{Kind: screen.Apply, ID: m.chrome.DialogID(), Values: map[string]string{"section": "Retry"}})
	if m.chrome.DialogID() != (screen.DialogID{}) || w.editor.Source() != before || len(w.commands) == 0 {
		t.Fatal("chooser changed the recipe or retained its dialog")
	}
	w.action(w.commands[len(w.commands)-1]().(workflowAction))
	if m.chrome.DialogID() == (screen.DialogID{}) || m.chrome.DialogID() == chooser || !strings.HasSuffix(m.chrome.DialogInfo().Title, "/ Retry") {
		t.Fatal("queued section did not open independently")
	}
	f := m.formSnapshot(t)
	for i, field := range f.Fields {
		if field.Key != "enabled" && m.chrome.FieldVisible(i) {
			t.Fatal("disabled retry exposed a policy field", field.Key)
		}
	}
	values := f.Values()
	values["enabled"] = "true"
	values["codes"] = `["timeout","rate_limit"]`
	values["max_attempts"] = "3"
	values["delay_ms"] = "250"
	err := m.applyValues(values)
	if err != nil {
		t.Fatal(err)
	}
	step := w.editor.Definition.Steps[0]
	if step.Retry == nil || step.Retry.MaxAttempts != 3 || step.Retry.DelayMS != 250 || strings.Join(step.Retry.Codes, ",") != "timeout,rate_limit" {
		t.Fatal("retry fields were not saved", step.Retry)
	}
	w.editor.Undo()
	if w.editor.Source() != before {
		t.Fatal("policy edit lost undo")
	}
}

func TestBehaviorFormsRemoveOptionalPoliciesAndKeepOtherSettings(t *testing.T) {
	for _, section := range []string{"Loop", "Retry", "Map", "Execution"} {
		t.Run(section, func(t *testing.T) {
			m := workflowTestModel(t)
			w := m.workflows
			d := workflowRecipe()
			step := &d.Steps[0]
			step.Repeat = &workflow.RepeatPolicy{MaxIterations: 3, Until: "/done", NextInputs: "/next"}
			step.Retry = &workflow.RetryPolicy{MaxAttempts: 2, DelayMS: 100}
			step.ForEach = json.RawMessage(`[{}]`)
			step.Concurrency = 2
			step.OnError = "collect"
			step.Review = true
			step.When = json.RawMessage(`true`)
			w.editor.Replace(d)
			w.graph.Selected = step.ID
			before := w.editor.Source()
			w.editBehavior(behaviorEdit{StepID: step.ID, Section: section, Draft: before})
			values := m.formSnapshot(t).Values()
			values["enabled"] = "false"
			values["for_each"] = ""
			values["when"] = ""
			values["review"] = "false"
			values["max_attempts"] = "invalid hidden value"
			values["max_iterations"] = "invalid hidden value"

			err := m.applyValues(values)
			if err != nil {
				t.Fatal(err)
			}
			got := w.editor.Definition.Steps[0]
			want := *step
			switch section {
			case "Loop":
				want.Repeat = nil
			case "Retry":
				want.Retry = nil
			case "Map":
				want.ForEach = nil
				want.Concurrency = 0
				want.OnError = ""
			case "Execution":
				want.Review = false
				want.When = nil
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("changed unrelated settings:\ngot  %#v\nwant %#v", got, want)
			}
		})
	}
}

func TestExecutionBehaviorEditsRoutesAndFinalizers(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	target := d.Steps[0]
	target.ID = "report"
	d.Steps = append(d.Steps, target)
	w.editor.Replace(d)
	w.graph.Selected = d.Steps[0].ID
	w.editBehavior(behaviorEdit{StepID: w.graph.Selected, Section: "Execution", Draft: w.editor.Source()})
	values := m.formSnapshot(t).Values()
	values["on_failure"] = "report"
	err := m.applyValues(values)
	if err != nil {
		t.Fatal(err)
	}
	if w.editor.Definition.Steps[0].OnFailure != "report" {
		t.Fatal("missing error route")
	}
	rows := strings.Join(w.detailRows(), "\n")
	if !strings.Contains(rows, "← report") {
		t.Fatal("inspector omitted error route", rows)
	}

	w.editBehavior(behaviorEdit{StepID: "report", Section: "Execution", Draft: w.editor.Source()})
	values = m.formSnapshot(t).Values()
	values["finally"] = "true"
	err = m.applyValues(values)
	if err != nil || !w.editor.Definition.Steps[1].Finally {
		t.Fatal("missing finalizer", err)
	}
}

func TestBehaviorSectionRejectsStaleDraftAndFrozenRun(t *testing.T) {
	for _, section := range []string{"Execution", "Map", "Loop", "Retry"} {
		for _, frozen := range []bool{false, true} {
			m := workflowTestModel(t)
			w := m.workflows
			w.editor.Replace(workflowRecipe())
			w.editBehavior(behaviorEdit{StepID: "transform", Section: section, Draft: w.editor.Source()})
			values := m.formSnapshot(t).Values()
			if frozen {
				w.run = &workflow.Run{}
			} else {
				changed := w.editor.Clone()
				changed.Steps = nil
				w.editor.Replace(changed)
			}
			before := w.editor.Source()
			err := m.applyValues(values)
			if err == nil || !strings.Contains(err.Error(), "draft changed") || w.editor.Source() != before {
				t.Fatal("section changed an unavailable draft", section, frozen, err)
			}
		}
	}
}
