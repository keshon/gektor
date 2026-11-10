package workflow

import (
	"encoding/json"
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestWorkflowPositionLockDoesNotDisableEditingOrPanning(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Replace(workflowRecipe())
	w.editor.Path = "recipe.json"
	m.workflowScreen()
	before := w.editor.Source()
	point := w.graph.Nodes[0].Point
	m.workflowCommand("lock-positions")
	m.workflows.key(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift})
	n := w.graph.Nodes[0]
	m.handleMouse(tea.MouseClickMsg{X: n.X + 3, Y: n.Y + 5, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseMotionMsg{X: n.X + 8, Y: n.Y + 7, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseReleaseMsg{X: n.X + 8, Y: n.Y + 7, Button: tea.MouseLeft})
	if w.graph.Nodes[0].Point != point || w.dragging {
		t.Fatal("locked node moved")
	}
	m.workflowCommand("layout")
	if w.graph.Nodes[0].Point != point {
		t.Fatal("locked auto-layout moved node")
	}
	m.handleMouse(tea.MouseClickMsg{X: 80, Y: 18, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseMotionMsg{X: 70, Y: 18, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseReleaseMsg{X: 70, Y: 18, Button: tea.MouseLeft})
	if w.graph.Pan.X != 10 {
		t.Fatal("locking disabled panning")
	}
	m.workflows.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog == nil || m.dialog.form == nil {
		t.Fatal("locking disabled argument editing")
	}
	m.dialog = nil
	w.positionsLocked = false
	m.workflows.loadLayout()
	if !w.positionsLocked {
		t.Fatal("lock was not saved")
	}
	m.workflows.key(tea.KeyPressMsg{Code: 'g'})
	m.workflows.key(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift})
	if w.graph.Nodes[0].X != point.X+2 {
		t.Fatal("unlock did not restore movement")
	}
	if w.editor.Source() != before {
		t.Fatal("layout changed executable recipe")
	}

}

func TestWorkflowIgnoresUnsupportedLayout(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	d.Steps[0].ID = "locked"
	w.editor.Replace(d)
	w.editor.Path = "recipe.json"
	m.workflowScreen()
	if err := m.workflows.saveLayout(); err != nil {
		t.Fatal(err)
	}
	before := w.graph.Nodes[0].Point
	unsupported := map[string]any{"locked": map[string]int{"x": 17, "y": 9}}
	b, _ := json.Marshal(unsupported)
	if err := os.WriteFile(m.workflows.layoutPath(), b, 0600); err != nil {
		t.Fatal(err)
	}
	w.positionsLocked = true
	m.workflows.loadLayout()
	if w.positionsLocked || w.graph.Nodes[0].Point != before {
		t.Fatal("unsupported layout changed the screen")
	}
}
