package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/workflow"
)

func TestInspectorPropertiesUseCardPortNamesAndSeparateCode(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	d.Steps[0].ID = "prepare"
	report := d.Steps[0]
	report.ID = "report"
	report.Args = json.RawMessage(`{"input":{"label":{"$step":"prepare","pointer":"/label"},"values":{"$step":"prepare","pointer":"/items"}},"code":"return input"}`)
	save := d.Steps[0]
	save.ID = "save"
	save.Tool = "artifact_write"
	save.Args = json.RawMessage(`{"name":"report.json","text":{"$step":"report","pointer":"/text"}}`)
	d.Steps = append(d.Steps, report, save)
	w.editor.Replace(d)
	w.graph.Selected = "report"
	m.workflowScreen()
	text := ansi.Strip(strings.Join(m.workflows.detailRows(), "\n"))
	for _, part := range []string{"JavaScript / Not run", "input.label", "← prepare.label", "input.values", "← prepare.items", "→ save.text", "No additional ordering", "Approval"} {
		if !strings.Contains(text, part) {
			t.Fatal("missing property", part, text)
		}
	}
	if strings.Contains(text, "return input") || strings.Contains(text, "Waits for:") {
		t.Fatal("properties contain code or inferred ordering", text)
	}
	m.workflowCommand("inspector")
	m.workflows.key(tea.KeyPressMsg{Code: '2'})
	if !strings.Contains(strings.Join(m.workflows.detailRows(), "\n"), "No recorded result for this draft") {
		t.Fatal("draft showed results")
	}
	m.workflows.key(tea.KeyPressMsg{Code: '3'})
	if !strings.Contains(strings.Join(m.workflows.detailRows(), "\n"), "return input") {
		t.Fatal("Code tab omitted script")
	}
	m.workflows.key(tea.KeyPressMsg{Code: '1'})
	before := w.editor.Source()
	m.workflows.key(tea.KeyPressMsg{Code: 'e'})
	for _, f := range m.chrome.Dialog.Form.Fields {
		if f.Key == "code" {
			t.Fatal("properties editor exposed code")
		}
	}
	if err := m.chrome.Dialog.Form.Apply(map[string]string{"input": `{"label":"changed","values":[]}`}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(w.editor.Definition.Steps[1].Args), `"code":"return input"`) {
		t.Fatal("properties edit removed script")
	}
	w.editor.Undo()
	if w.editor.Source() != before {
		t.Fatal("property undo lost data")
	}
}

func TestInspectorTabsArePinnedAndClickableAtEveryWidth(t *testing.T) {
	for _, width := range []int{40, 80, 121} {
		m := workflowTestModel(t)
		sizeModelTo(t, m, width, 24)
		w := m.workflows
		d := workflowRecipe()
		long := strings.Repeat("line\n", 100)
		args := map[string]any{"code": long, "input": 2}
		d.Steps[0].Args, _ = json.Marshal(args)
		w.editor.Replace(d)
		m.workflowScreen()
		m.workflowCommand("inspector")
		left := 0
		if width >= 100 {
			left = width / 2
		}
		for tab := 0; tab < 3; tab++ {
			tabs := m.workflows.inspectorTabs()
			hit := tabs.Hits[tab]
			m.handleMouse(tea.MouseClickMsg{X: left + 1 + hit.X, Y: paneTop + hit.Y, Button: tea.MouseLeft})
			if w.inspectorTab != tab || !w.inspect {
				t.Fatal("tab click missed", width, tab)
			}
		}
		m.workflows.key(tea.KeyPressMsg{Code: tea.KeyPgDown})
		rows := strings.Split(ansi.Strip(m.workflowScreen()), "\n")
		if !strings.Contains(rows[paneTop], "[Code]") || w.offset == 0 {
			t.Fatal("tabs scrolled with content", width)
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != width {
				t.Fatal("tab overflow", width, row)
			}
		}
		m.workflows.key(tea.KeyPressMsg{Code: tea.KeyLeft})
		if w.inspectorTab != 1 || w.offset != 0 {
			t.Fatal("tab switch kept stale offset")
		}
	}
}

func TestCodeEditorPreservesArgumentsRejectsStaleAndFrozenEdits(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Replace(workflowRecipe())
	m.workflowScreen()
	m.workflowCommand("inspector")
	m.workflows.setInspectorTab(2)
	before := w.editor.Source()
	m.workflows.key(tea.KeyPressMsg{Code: 'e'})
	f := m.chrome.Dialog.Form
	if len(f.Fields) != 1 || f.Fields[0].Key != "code" {
		t.Fatal("not a dedicated code editor")
	}
	if err := f.Apply(map[string]string{"code": "return input + 9;"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(w.editor.Definition.Steps[0].Args), `"input":2`) {
		t.Fatal("code edit changed inputs")
	}
	w.editor.Undo()
	if w.editor.Source() != before {
		t.Fatal("code undo failed")
	}
	m.chrome.Dialog = nil
	m.workflows.editCode()
	f = m.chrome.Dialog.Form
	d := w.editor.Clone()
	d.Name = "Changed"
	w.editor.Replace(d)
	if err := f.Apply(map[string]string{"code": "stale"}); err == nil {
		t.Fatal("accepted stale script")
	}
	m.chrome.Dialog = nil
	w.run = &workflow.Run{RunID: "recorded", Definition: workflowRecipe(), Steps: []workflow.StepResult{{ID: "transform", Status: "completed", Args: json.RawMessage(`{"code":"return 42;","input":7}`), Output: `42`}}}
	m.workflows.setInspectorTab(1)
	text := strings.Join(m.workflows.detailRows(), "\n")
	if !strings.Contains(text, "recorded") || !strings.Contains(text, "42") || strings.Contains(text, "return 42") {
		t.Fatal(text)
	}
	m.workflows.setInspectorTab(2)
	if !strings.Contains(strings.Join(m.workflows.detailRows(), "\n"), "return 42;") {
		t.Fatal("code tab did not use recorded script")
	}
	m.workflows.key(tea.KeyPressMsg{Code: 'e'})
	if m.chrome.Dialog != nil {
		t.Fatal("frozen script was editable")
	}
}

func TestPortSelectionFocusesMatchingPropertyAndKeepsFrozenProperties(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	d.Steps[0].Args = json.RawMessage(`{"input":{"numbers":{"$input":"numbers"}},"code":"return input"}`)
	w.editor.Replace(d)
	m.workflowScreen()
	m.workflowCommand("inspector")
	clickWorkflowPort(t, m, "transform", "args/input/numbers", false)
	if w.inspectorTab != 0 || !w.portSelected || w.portFocus != "args/input/numbers" {
		t.Fatal("port did not focus Properties")
	}
	text := ansi.Strip(strings.Join(m.workflows.detailRows(), "\n"))
	if !strings.Contains(text, "› input.numbers") {
		t.Fatal(text)
	}
	source, _ := json.Marshal(d)
	w.run = &workflow.Run{RunID: "frozen", Definition: d, RecipeSource: string(source)}
	changed := workflowRecipe()
	changed.Steps[0].Args = json.RawMessage(`{"input":"different draft","code":"return 99"}`)
	w.editor.Replace(changed)
	m.workflows.setInspectorTab(0)
	text = ansi.Strip(strings.Join(m.workflows.detailRows(), "\n"))
	if strings.Contains(text, "different draft") || !strings.Contains(text, "← Workflow input numbers") || !strings.Contains(text, "Frozen recipe / frozen") {
		t.Fatal("properties used mutable draft", text)
	}
}

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
	d.Inputs = map[string]workflow.Input{"numbers": {Type: "array", Default: json.RawMessage(`[2,3,5]`)}, "label": {Type: "string", Default: json.RawMessage(`"agent-smoke"`)}}
	m.workflows.editor.Replace(d)
	m.workflowCommand("run")
	if m.chrome.Dialog == nil || m.chrome.Dialog.Title != "Run workflow" {
		t.Fatal("did not open run values directly")
	}
	f := m.chrome.Dialog.Form
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
	if err := f.Apply(values); err != nil {
		t.Fatal(err)
	}
	if string(queuedInvocation(t, m).Inputs) != "{}" {
		t.Fatal("untouched defaults were overridden")
	}
	values["value:numbers"] = "4, 6, 8"
	values["value:label"] = ""
	if err := f.Apply(values); err != nil {
		t.Fatal(err)
	}
	var inputs map[string]json.RawMessage
	_ = json.Unmarshal(queuedInvocation(t, m).Inputs, &inputs)
	if string(inputs["numbers"]) != "[4,6,8]" || string(inputs["label"]) != `""` {
		t.Fatal(string(queuedInvocation(t, m).Inputs))
	}
	values["value:numbers"] = "not a list"
	if err := f.Apply(values); err == nil {
		t.Fatal("accepted invalid list")
	}
	m.chrome.Dialog = nil
	m.workflowCommand("run-json")
	if m.chrome.Dialog == nil || len(m.chrome.Dialog.Form.Fields) != 1 || m.chrome.Dialog.Form.Fields[0].Key != "inputs" {
		t.Fatal("advanced JSON unavailable")
	}
}

func TestNoInputWorkflowQueuesRunWithoutEmptyObjectDialog(t *testing.T) {
	m := workflowTestModel(t)
	m.workflows.editor.Replace(workflowRecipe())
	m.workflowCommand("run")
	if m.chrome.Dialog != nil || string(queuedInvocation(t, m).Inputs) != "{}" {
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
	before := (&Editor{Definition: d}).Graph(graph.Model{})
	compiled := d
	compiled.Steps = append(append([]workflow.Step(nil), d.Steps...), workflow.Step{ID: "@outputs", Tool: "workflow_value", CollectOutputs: true, DependsOn: []string{"transform", "second"}, Args: json.RawMessage(`{}`)})
	w.runID = "saved"
	w.run = &workflow.Run{RunID: "saved", Definition: compiled, RecipeSource: string(source), Steps: []workflow.StepResult{{ID: "transform", Status: "completed"}, {ID: "second", Status: "completed"}}}
	m.workflows.acceptPoll(workflowPoll{id: "saved", run: w.run})
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
	m.workflows.ensureSelection()
	text := strings.Join(m.workflows.detailRows(), "\n")
	if !strings.Contains(text, "input.numbers") || !strings.Contains(text, "← Workflow input numbers") || strings.Contains(text, "return input.numbers;") || strings.Contains(text, `"$input"`) {
		t.Fatal(text)
	}
	m.workflows.setInspectorTab(2)
	if !strings.Contains(strings.Join(m.workflows.detailRows(), "\n"), "return input.numbers;") {
		t.Fatal("script missing from Code tab")
	}
	m.workflowCommand("raw-details")
	raw := strings.Join(m.workflows.detailRows(), "\n")
	if !strings.Contains(raw, `"$input"`) || !strings.Contains(raw, `"code"`) {
		t.Fatal("lost exact raw data", raw)
	}
}

func TestWorkflowTabGapAndWheelDoNotChangePage(t *testing.T) {
	m := workflowTestModel(t)
	m.workflows.editor.Replace(workflowRecipe())
	m.workflowScreen()
	m.workflowCommand("inspector")
	tabs := m.workflows.inspectorTabs()
	area := m.screenPanes(true).Side
	layout := m.panelGeometry(area.Width, 1, true, false)
	x, y := area.X+layout.Body.X, area.Y+layout.Body.Y
	m.workflows.offset = 3
	m.handleMouse(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	if m.workflows.offset != 3 {
		t.Fatal("wheel over pinned tabs scrolled their page")
	}
	m.handleMouse(tea.MouseClickMsg{X: x + tabs.Hits[0].Width, Y: y, Button: tea.MouseLeft})
	if m.workflows.inspectorTab != 0 || m.workflows.offset != 3 {
		t.Fatal("tab gap changed the page")
	}
}
