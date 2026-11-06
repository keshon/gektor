package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/tui/components/graph"
)

func clickWorkflowPort(t *testing.T, m *model, id, key string, output bool) {
	t.Helper()
	for _, n := range m.workflows.graph.Nodes {
		if n.ID != id {
			continue
		}
		ports := n.Inputs
		x := n.X
		if output {
			ports = n.Outputs
			x += graph.NodeWidth - 1
		}
		for i, p := range ports {
			if p.Key == key {
				m.handleMouse(tea.MouseClickMsg{X: 1 + x - m.workflows.graph.Pan.X, Y: paneTop + n.Y + 4 + i - m.workflows.graph.Pan.Y, Button: tea.MouseLeft})
				return
			}
		}
	}
	t.Fatalf("missing port %s/%s", id, key)
}

func TestNamedPortClicksPreserveExactSourceAndDestination(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	d.Steps[0].ID = "source"
	target := d.Steps[0]
	target.ID = "target"
	target.Args = json.RawMessage(`{"input":{"label":"old"},"code":"return input"}`)
	d.Steps = append(d.Steps, target)
	d.Outputs = map[string]json.RawMessage{"label": json.RawMessage(`{"$step":"source","pointer":"/label"}`)}
	w.editor.Replace(d)
	m.workflowScreen()
	// Separate the two independent nodes so both ports fit in this test viewport.
	w.graph.Nodes[1].Point = graph.Point{X: 65, Y: 1}
	before := w.editor.Source()
	clickWorkflowPort(t, m, "source", "/label", true)
	clickWorkflowPort(t, m, "target", "args/input/label", false)
	if m.dialog == nil || m.dialog.form == nil {
		t.Fatal("named ports did not open mapping")
	}
	for _, f := range m.dialog.form.Fields {
		if f.Key == "pointer" || f.Key == "source_field" {
			t.Fatal("named mapping lost its source selection")
		}
	}
	if err := m.dialog.form.apply(map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(w.editor.Definition.Steps[1].Args), `"label":{"$step":"source","pointer":"/label"}`) {
		t.Fatal(string(w.editor.Definition.Steps[1].Args))
	}
	w.editor.Undo()
	if w.editor.Source() != before {
		t.Fatal("named mapping undo lost data")
	}
	m.dialog = nil
	clickWorkflowPort(t, m, "source", "@order", true)
	clickWorkflowPort(t, m, "target", "args/input/label", false)
	if m.dialog != nil || !strings.Contains(w.notice, "data ports") {
		t.Fatal("mixed execution and data ports")
	}
	clickWorkflowPort(t, m, "target", "@order", false)
	if m.dialog == nil {
		t.Fatal("ordering ports did not map")
	}
	if err := m.dialog.form.apply(map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if len(w.editor.Definition.Steps[1].DependsOn) != 1 || w.editor.Definition.Steps[1].DependsOn[0] != "source" {
		t.Fatal("wrong execution dependency")
	}
	if string(w.editor.Definition.Steps[1].Args) != string(target.Args) {
		t.Fatal("ordering changed arguments")
	}
}
