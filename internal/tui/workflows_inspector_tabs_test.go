package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/automation"
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
	text := ansi.Strip(strings.Join(m.workflowDetailRows(), "\n"))
	for _, part := range []string{"JavaScript / Not run", "input.label", "← prepare.label", "input.values", "← prepare.items", "→ save.text", "No additional ordering", "Approval"} {
		if !strings.Contains(text, part) {
			t.Fatal("missing property", part, text)
		}
	}
	if strings.Contains(text, "return input") || strings.Contains(text, "Waits for:") {
		t.Fatal("properties contain code or inferred ordering", text)
	}
	m.workflowCommand("inspector")
	m.workflowKey(tea.KeyPressMsg{Code: '2'})
	if !strings.Contains(strings.Join(m.workflowDetailRows(), "\n"), "No recorded result for this draft") {
		t.Fatal("draft showed results")
	}
	m.workflowKey(tea.KeyPressMsg{Code: '3'})
	if !strings.Contains(strings.Join(m.workflowDetailRows(), "\n"), "return input") {
		t.Fatal("Code tab omitted script")
	}
	m.workflowKey(tea.KeyPressMsg{Code: '1'})
	before := w.editor.Source()
	m.workflowKey(tea.KeyPressMsg{Code: 'e'})
	for _, f := range m.dialog.form.Fields {
		if f.Key == "code" {
			t.Fatal("properties editor exposed code")
		}
	}
	if err := m.dialog.form.apply(map[string]string{"input": `{"label":"changed","values":[]}`}); err != nil {
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
		if width >= inspectorSplitWidth {
			left = width / 2
		}
		for tab := 0; tab < 3; tab++ {
			_, ends := m.workflowInspectorTabs()
			start := 0
			if tab > 0 {
				start = ends[tab-1] + 2
			}
			m.handleMouse(tea.MouseClickMsg{X: left + 1 + start, Y: paneTop, Button: tea.MouseLeft})
			if w.inspectorTab != tab || !w.inspect {
				t.Fatal("tab click missed", width, tab)
			}
		}
		m.workflowKey(tea.KeyPressMsg{Code: tea.KeyPgDown})
		rows := strings.Split(ansi.Strip(m.workflowScreen()), "\n")
		if !strings.Contains(rows[paneTop], "[Code]") || w.offset == 0 {
			t.Fatal("tabs scrolled with content", width)
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != width {
				t.Fatal("tab overflow", width, row)
			}
		}
		m.workflowKey(tea.KeyPressMsg{Code: tea.KeyLeft})
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
	m.workflowSetInspectorTab(2)
	before := w.editor.Source()
	m.workflowKey(tea.KeyPressMsg{Code: 'e'})
	f := m.dialog.form
	if len(f.Fields) != 1 || f.Fields[0].Key != "code" {
		t.Fatal("not a dedicated code editor")
	}
	if err := f.apply(map[string]string{"code": "return input + 9;"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(w.editor.Definition.Steps[0].Args), `"input":2`) {
		t.Fatal("code edit changed inputs")
	}
	w.editor.Undo()
	if w.editor.Source() != before {
		t.Fatal("code undo failed")
	}
	m.dialog = nil
	m.workflowEditCode()
	f = m.dialog.form
	d := w.editor.Clone()
	d.Name = "Changed"
	w.editor.Replace(d)
	if err := f.apply(map[string]string{"code": "stale"}); err == nil {
		t.Fatal("accepted stale script")
	}
	m.dialog = nil
	w.run = &automation.Run{RunID: "recorded", Definition: workflowRecipe(), Steps: []automation.StepResult{{ID: "transform", Status: "completed", Args: json.RawMessage(`{"code":"return 42;","input":7}`), Output: `42`}}}
	m.workflowSetInspectorTab(1)
	text := strings.Join(m.workflowDetailRows(), "\n")
	if !strings.Contains(text, "recorded") || !strings.Contains(text, "42") || strings.Contains(text, "return 42") {
		t.Fatal(text)
	}
	m.workflowSetInspectorTab(2)
	if !strings.Contains(strings.Join(m.workflowDetailRows(), "\n"), "return 42;") {
		t.Fatal("code tab did not use recorded script")
	}
	m.workflowKey(tea.KeyPressMsg{Code: 'e'})
	if m.dialog != nil {
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
	text := ansi.Strip(strings.Join(m.workflowDetailRows(), "\n"))
	if !strings.Contains(text, "› input.numbers") {
		t.Fatal(text)
	}
	source, _ := json.Marshal(d)
	w.run = &automation.Run{RunID: "frozen", Definition: d, RecipeSource: string(source)}
	changed := workflowRecipe()
	changed.Steps[0].Args = json.RawMessage(`{"input":"different draft","code":"return 99"}`)
	w.editor.Replace(changed)
	m.workflowSetInspectorTab(0)
	text = ansi.Strip(strings.Join(m.workflowDetailRows(), "\n"))
	if strings.Contains(text, "different draft") || !strings.Contains(text, "← Workflow input numbers") || !strings.Contains(text, "Frozen recipe / frozen") {
		t.Fatal("properties used mutable draft", text)
	}
}
