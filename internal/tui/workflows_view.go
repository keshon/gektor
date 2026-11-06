package tui

import (
	"encoding/json"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/recipe"
	editor "github.com/keshon/gektor/internal/tui/workflow"
)

func (m *model) workflowDefinition() automation.Definition {
	if m.workflows.run != nil {
		// The execution definition contains compiler bookkeeping. Orientation uses
		// the frozen authoring snapshot, never the current workspace file.
		if d, err := editor.Decode([]byte(m.workflows.run.RecipeSource)); err == nil {
			return d
		}
		return m.workflows.run.Definition
	}
	return m.workflows.editor.Definition
}

func (m *model) workflowSteps() ([]automation.Step, error) {
	d := m.workflowDefinition()
	steps, err := recipe.Order(d.Steps)
	if err != nil {
		return d.Steps, err
	}
	return steps, nil
}

func (m *model) workflowEnsureSelection() {
	steps, _ := m.workflowSteps()
	for _, step := range steps {
		if step.ID == m.workflows.graph.Selected {
			return
		}
	}
	m.workflows.graph.Selected = ""
	m.workflows.portSelected = false
	if len(steps) > 0 {
		m.workflows.graph.Selected = steps[0].ID
	}
	m.workflows.offset = 0
}

func workflowValue(raw json.RawMessage) string {
	var ref map[string]json.RawMessage
	if json.Unmarshal(raw, &ref) == nil {
		for _, kind := range []string{"$input", "$step"} {
			var name, pointer string
			if json.Unmarshal(ref[kind], &name) == nil && name != "" {
				_ = json.Unmarshal(ref["pointer"], &pointer)
				prefix := "Input "
				if kind == "$step" {
					prefix = "Step "
				}
				return prefix + name + pointer
			}
		}
	}
	return string(raw)
}

func (m *model) workflowPage(key string) {
	w := m.workflows
	_, height := m.workflowCanvasSize()
	if w.inspect {
		height = max(1, height-2)
		switch key {
		case "home":
			w.offset = 0
		case "end":
			w.offset = max(0, len(m.workflowInspectorContent())-height)
		case "pgup":
			w.offset = max(0, w.offset-height)
		case "pgdown":
			w.offset += height
		}
		return
	}
	if len(w.graph.Nodes) == 0 {
		return
	}
	switch key {
	case "home":
		w.graph.Selected = w.graph.Nodes[0].ID
	case "end":
		w.graph.Selected = w.graph.Nodes[len(w.graph.Nodes)-1].ID
	case "pgup":
		w.graph.SelectNext(-1)
	case "pgdown":
		w.graph.SelectNext(1)
	}
	width, height := m.workflowCanvasSize()
	w.graph.Reveal(width, height)
}
