package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func submitAuthoringForm(t *testing.T, m *model, values map[string]string) tea.Cmd {
	t.Helper()
	if err := m.applyValues(values); err != nil {
		t.Fatal(err)
	}
	m.closeDialog()
	w := m.workflows
	if len(w.commands) == 0 {
		return nil
	}
	command := w.commands[0]
	w.commands = w.commands[1:]
	return w.action(command().(workflowAction))
}

func TestRecipeAuthoringFromEmptyCanvas(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	clickCanvasRow := func(y int) {
		t.Helper()
		layout := m.panelGeometry(m.termW, 1, false, true)
		m.handleMouse(tea.MouseClickMsg{X: layout.Body.X + 1, Y: 1 + layout.Body.Y + y, Button: tea.MouseLeft})
	}

	clickCanvasRow(1)
	submitAuthoringForm(t, m, map[string]string{"path": "draft.json"})
	canvas := ansi.Strip(m.workflowScreen())
	if !strings.Contains(canvas, "Untitled workflow / draft.json") || !strings.Contains(canvas, firstNodeLabel) {
		t.Fatal("new draft does not expose its path and first action")
	}
	clickCanvasRow(1)
	for _, id := range []string{"first", "second"} {
		submitAuthoringForm(t, m, map[string]string{"type": "script_transform"})
		submitAuthoringForm(t, m, map[string]string{"$id": id, "input": "2", "code": "return input + 1;"})
		if m.chrome.DialogInfo().Kind != screen.NoDialog {
			t.Fatal("node creation opened another arguments form")
		}
		if id == "first" {
			m.workflowCommand("add")
		}
	}
	w.graph.Selected = "first"
	m.workflowCommand("link")
	submitAuthoringForm(t, m, map[string]string{"destination": "second / Argument: input"})
	if !strings.HasPrefix(m.chrome.DialogInfo().Title, "Map first") {
		t.Fatal("destination did not go straight to mapping review")
	}
	submitAuthoringForm(t, m, map[string]string{"pointer": ""})
	if !strings.Contains(string(w.editor.Definition.Steps[1].Args), `"$step":"first"`) {
		t.Fatal("connection was not applied")
	}

	m.workflowCommand("validate")
	command := w.commands[0]
	w.commands = w.commands[1:]
	result := w.action(command().(workflowAction))().(workflowResult)
	if result.err != nil || !result.outcome.Valid {
		t.Fatalf("recipe did not validate: %+v", result)
	}
	w.acceptResult(result)

	m.workflowCommand("save")
	save := submitAuthoringForm(t, m, map[string]string{"path": "draft.json"})
	result = save().(workflowResult)
	if result.err != nil {
		t.Fatal(result.err)
	}
	refresh := w.acceptResult(result)
	if refresh != nil {
		w.acceptResult(refresh().(workflowResult))
	}

	m.workflowCommand("run")
	command = w.commands[0]
	w.commands = w.commands[1:]
	msg := w.action(command().(workflowAction))()
	if id, ok := msg.(workflowRunID); ok {
		w.runID = string(id)
		select {
		case msg = <-w.events:
		case <-time.After(5 * time.Second):
			t.Fatal("run did not finish")
		}
	}
	result, ok := msg.(workflowResult)
	if !ok || result.err != nil || result.outcome.Status != "completed" {
		t.Fatalf("saved recipe did not complete its first run: %+v", msg)
	}
}

func TestRecipeSaveRequiresConfirmationAndRejectsRacingFile(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Replace(workflowRecipe())
	w.editor.Path = "recipe.json"
	path := filepath.Join(m.wsRoot, w.editor.Path)
	if err := os.WriteFile(path, []byte("existing file"), 0600); err != nil {
		t.Fatal(err)
	}

	m.workflowCommand("save")
	submitAuthoringForm(t, m, map[string]string{"path": "recipe.json"})
	confirm := m.formSnapshot(t)
	if !confirm.Destructive || confirm.Index != len(confirm.Fields)+1 {
		t.Fatal("replacement must require explicit confirmation")
	}
	m.closeDialog()
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "existing file" {
		t.Fatalf("cancelled replacement changed the file: %q, %v", raw, err)
	}

	result := w.action(workflowAction{"save-path", "recipe.json"})().(workflowResult)
	if !os.IsExist(result.err) {
		t.Fatalf("new-path save did not refuse the collision: %v", result.err)
	}
	w.acceptResult(result)
	submitAuthoringForm(t, m, map[string]string{"path": "recipe.json"})
	cmd := submitAuthoringForm(t, m, nil)
	result = cmd().(workflowResult)
	if result.err != nil {
		t.Fatal(result.err)
	}
	raw, err = os.ReadFile(path)
	if err != nil || !json.Valid(raw) {
		t.Fatalf("confirmed replacement did not write the recipe: %q, %v", raw, err)
	}
}

func TestGuidedRecipeReferencesAndValidatedInputReuse(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	d.Inputs = map[string]workflow.Input{"count": {Type: "integer", Required: true}}
	w.editor.Replace(d)
	w.graph.Selected = "transform"
	m.workflowCommand("bind-input")
	submitAuthoringForm(t, m, map[string]string{"argument": "input", "input": "Recipe input / count"})
	if !strings.Contains(string(w.editor.Definition.Steps[0].Args), `"$input":"count"`) {
		t.Fatal("selected recipe input was not bound")
	}
	w.parameterEdit(workflowParameterEdit{Kind: "outputs", Draft: w.editor.Source()})
	options := m.formSnapshot(t).Choices["source"]
	source := "Node output / transform / whole output"
	if !strings.Contains(strings.Join(options, "\n"), source) {
		t.Fatal("node result is not selectable as a recipe output")
	}
	submitAuthoringForm(t, m, map[string]string{"name": "value", "source": source})
	if !strings.Contains(string(w.editor.Definition.Outputs["value"]), `"$step":"transform"`) {
		t.Fatal("selected result was not exposed as an output")
	}

	m.workflowCommand("validate")
	cmd := submitAuthoringForm(t, m, map[string]string{"value:count": "9007199254740993"})
	result := cmd().(workflowResult)
	if result.err != nil || !result.outcome.Valid {
		t.Fatalf("recipe did not validate: %+v", result)
	}
	w.acceptResult(result)
	m.workflowCommand("run")
	if got := m.formSnapshot(t).Values()["value:count"]; got != "9007199254740993" {
		t.Fatalf("run did not inherit the validated input: %q", got)
	}
	m.closeDialog()
	changed := w.editor.Clone()
	changed.Name = "Changed draft"
	w.editor.Replace(changed)
	m.workflowCommand("run")
	if got := m.formSnapshot(t).Values()["value:count"]; got != "" {
		t.Fatalf("changed recipe inherited an older validation input: %q", got)
	}
}
