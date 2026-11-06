package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/workflow"
)

func TestWorkflowInspectionDistinguishesNullFromEmptyText(t *testing.T) {
	if got := propertyValue(json.RawMessage(`null`)); got != "null" {
		t.Fatal("null property shown as text", got)
	}
	if got := propertyValue(json.RawMessage(`""`)); got != "(empty text)" {
		t.Fatal("empty text lost its label", got)
	}
	if got := strings.Join(workflowValues(json.RawMessage(`{"data":null,"text":""}`)), "\n"); !strings.Contains(got, "data: null") || !strings.Contains(got, "(empty text)") {
		t.Fatal("recorded null and empty text are indistinguishable", got)
	}
}

func TestRunUsesOnePrefilledFormAndSimpleLists(t *testing.T) {
	m := workflowTestModel(t)
	d := workflowRecipe()
	d.Inputs = map[string]automation.Input{"numbers": {Type: "array", Default: json.RawMessage(`[2,3,5]`)}, "label": {Type: "string", Default: json.RawMessage(`"agent-smoke"`)}}
	m.workflows.editor.Replace(d)
	m.workflowCommand("run")
	if m.dialog == nil || m.dialog.title != "Run workflow" {
		t.Fatal("did not open run values directly")
	}
	f := m.dialog.form
	if len(f.Fields) != 2 {
		t.Fatal("unexpected format or value-source controls", f.Fields)
	}
	values := map[string]string{}
	for _, field := range f.Fields {
		values[field.Key] = field.Value
	}
	if values["value:label"] != "agent-smoke" || values["value:numbers"] != "2, 3, 5" {
		t.Fatal(values)
	}
	if err := f.apply(values); err != nil {
		t.Fatal(err)
	}
	if string(queuedInvocation(t, m).Inputs) != "{}" {
		t.Fatal("untouched defaults were overridden")
	}
	values["value:numbers"] = "4, 6, 8"
	values["value:label"] = ""
	if err := f.apply(values); err != nil {
		t.Fatal(err)
	}
	var inputs map[string]json.RawMessage
	_ = json.Unmarshal(queuedInvocation(t, m).Inputs, &inputs)
	if string(inputs["numbers"]) != "[4,6,8]" || string(inputs["label"]) != `""` {
		t.Fatal(string(queuedInvocation(t, m).Inputs))
	}
	values["value:numbers"] = "not a list"
	if err := f.apply(values); err == nil {
		t.Fatal("accepted invalid list")
	}
	m.dialog = nil
	m.workflowCommand("run-json")
	if m.dialog == nil || len(m.dialog.form.Fields) != 1 || m.dialog.form.Fields[0].Key != "inputs" {
		t.Fatal("advanced JSON unavailable")
	}
}

func TestNoInputWorkflowQueuesRunWithoutEmptyObjectDialog(t *testing.T) {
	m := workflowTestModel(t)
	m.workflows.editor.Replace(workflowRecipe())
	m.workflowCommand("run")
	if m.dialog != nil || string(queuedInvocation(t, m).Inputs) != "{}" {
		t.Fatal("unnecessary no-input dialog")
	}
}

func TestSavedDiagramUsesFrozenRecipeWithoutCollectorLinks(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	second := d.Steps[0]
	second.ID = "second"
	second.Args = json.RawMessage(`{"input":{"$step":"transform"},"code":"return input"}`)
	d.Steps = append(d.Steps, second)
	source, _ := json.Marshal(d)
	before := (&workflow.Editor{Definition: d}).Graph(graph.Model{})
	compiled := d
	compiled.Steps = append(append([]automation.Step(nil), d.Steps...), automation.Step{ID: "@outputs", Tool: "workflow_value", CollectOutputs: true, DependsOn: []string{"transform", "second"}, Args: json.RawMessage(`{}`)})
	w.runID = "saved"
	w.run = &automation.Run{RunID: "saved", Definition: compiled, RecipeSource: string(source), Steps: []automation.StepResult{{ID: "transform", Status: "completed"}, {ID: "second", Status: "completed"}}}
	m.acceptWorkflowPoll(workflowPoll{id: "saved", run: w.run})
	if len(w.graph.Nodes) != len(before.Nodes) || len(w.graph.Edges) != len(before.Edges) {
		t.Fatal("bookkeeping changed diagram", w.graph)
	}
	for _, node := range w.graph.Nodes {
		if node.Detail != "completed" {
			t.Fatal("lost actual status", node)
		}
	}
	if len(w.run.Definition.Steps) != 3 {
		t.Fatal("display changed execution record")
	}
}

func TestWorkflowInspectorSeparatesValuesReferencesAndCode(t *testing.T) {
	m := workflowTestModel(t)
	d := workflowRecipe()
	d.Steps[0].Args = json.RawMessage(`{"input":{"numbers":{"$input":"numbers"}},"code":"return input.numbers;"}`)
	m.workflows.editor.Replace(d)
	m.workflowEnsureSelection()
	text := strings.Join(m.workflowDetailRows(), "\n")
	if !strings.Contains(text, "input.numbers") || !strings.Contains(text, "← Workflow input numbers") || strings.Contains(text, "return input.numbers;") || strings.Contains(text, `"$input"`) {
		t.Fatal(text)
	}
	m.workflowSetInspectorTab(2)
	if !strings.Contains(strings.Join(m.workflowDetailRows(), "\n"), "return input.numbers;") {
		t.Fatal("script missing from Code tab")
	}
	m.workflowCommand("raw-details")
	raw := strings.Join(m.workflowDetailRows(), "\n")
	if !strings.Contains(raw, `"$input"`) || !strings.Contains(raw, `"code"`) {
		t.Fatal("lost exact raw data", raw)
	}
}
