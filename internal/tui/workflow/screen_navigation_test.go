package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/graph"
)

func TestWorkflowFormRequestsPreserveRemovalFocus(t *testing.T) {
	m := workflowTestModel(t)
	m.workflows.editor.Replace(workflowRecipe())
	m.workflows.graph.Selected = "transform"
	m.workflowCommand("delete")
	if !m.dialog.form.Destructive || m.dialog.form.Index != 0 {
		t.Fatal("typed confirmation no longer starts at its input")
	}
}

func TestWorkflowCanvasPanAndNodeDragStaySeparate(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Replace(workflowRecipe())
	w.graph = w.editor.Graph(graph.Model{})
	before := w.editor.Source()
	point := w.graph.Nodes[0].Point
	m.handleMouse(tea.MouseClickMsg{X: 80, Y: 18, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseMotionMsg{X: 69, Y: 14, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseReleaseMsg{X: 69, Y: 14, Button: tea.MouseLeft})
	if w.graph.Pan.X != 11 || w.graph.Pan.Y != 4 || w.graph.Nodes[0].Point != point || w.editor.Source() != before {
		t.Fatal("pan changed node or recipe", w.graph)
	}
	m.handleMouse(tea.MouseWheelMsg{X: 70, Y: 15, Button: tea.MouseWheelRight})
	if w.graph.Pan.X != 14 {
		t.Fatal("horizontal wheel ignored")
	}
	m.handleMouse(tea.MouseWheelMsg{X: 70, Y: 15, Button: tea.MouseWheelDown, Mod: tea.ModShift})
	if w.graph.Pan.X != 17 || w.graph.Pan.Y != 4 {
		t.Fatal("shift wheel did not pan horizontally")
	}
	w.graph.Pan = graph.Point{}
	n := w.graph.Nodes[0]
	m.handleMouse(tea.MouseClickMsg{X: n.X + 3, Y: n.Y + 5, Button: tea.MouseMiddle})
	m.handleMouse(tea.MouseMotionMsg{X: n.X + 9, Y: n.Y + 7, Button: tea.MouseMiddle})
	m.handleMouse(tea.MouseReleaseMsg{X: n.X + 9, Y: n.Y + 7, Button: tea.MouseMiddle})
	if w.graph.Pan.X != -6 || w.graph.Pan.Y != -2 || w.graph.Nodes[0].Point != point {
		t.Fatal("middle drag moved node", w.graph)
	}
	w.graph.Pan = graph.Point{}
	m.handleMouse(tea.MouseClickMsg{X: n.X + 3, Y: n.Y + 5, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseMotionMsg{X: n.X + 7, Y: n.Y + 7, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseReleaseMsg{X: n.X + 7, Y: n.Y + 7, Button: tea.MouseLeft})
	if w.graph.Nodes[0].X != point.X+4 || w.graph.Nodes[0].Y != point.Y+2 || w.graph.Pan != (graph.Point{}) || w.editor.Source() != before {
		t.Fatal("node drag changed pan or recipe", w.graph)
	}
}

func TestWorkflowInspectorCloseAndResponsiveLayout(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Replace(workflowRecipe())
	d := w.editor.Clone()
	long := strings.Repeat("abcdefghij", 40)
	d.Steps[0].Args = json.RawMessage(`{"code":"` + long + `","input":2}`)
	w.editor.Replace(d)
	full, _ := m.workflows.canvasSize()
	m.workflowCommand("inspector")
	split, _ := m.workflows.canvasSize()
	if split >= full || !w.inspectorOpen || !w.inspect {
		t.Fatal("inspector did not open")
	}
	m.workflows.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if w.inspectorOpen || !w.active {
		t.Fatal("Esc left workflow instead of closing inspector")
	}
	for _, width := range []int{120, 80, 40} {
		sizeModelTo(t, m, width, 24)
		m.workflowCommand("inspector")
		m.workflows.setInspectorTab(2)
		rows := strings.Split(ansi.Strip(m.screenContent()), "\n")
		if len(rows) != 24 {
			t.Fatal("height")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != width {
				t.Fatal("width", row)
			}
		}
		if width < 100 {
			cw, _ := m.workflows.canvasSize()
			if cw != width-2 {
				t.Fatal("narrow inspector shrank canvas")
			}
		}
		if !strings.Contains(rows[2], "×") {
			t.Fatal("close control missing", rows[2])
		}
		if !strings.Contains(strings.Join(m.workflows.inspectorContent(), ""), long) {
			t.Fatal("inspector truncated a long value")
		}
		m.handleMouse(tea.MouseClickMsg{X: width - 4, Y: 2, Button: tea.MouseLeft})
		if w.inspectorOpen {
			t.Fatal("close control did not close inspector")
		}
	}
}

func TestWorkflowPortsOpenMapping(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	second := d.Steps[0]
	second.ID = "second"
	d.Steps = append(d.Steps, second)
	w.editor.Replace(d)
	w.graph = w.editor.Graph(graph.Model{})
	a, b := w.graph.Nodes[0], w.graph.Nodes[1]
	m.handleMouse(tea.MouseClickMsg{X: 1 + a.X + graph.NodeWidth - 1, Y: 4 + a.Y + graph.NodeHeight/2, Button: tea.MouseLeft})
	if w.connectFrom != a.ID || w.dragging {
		t.Fatal("output port started a drag")
	}
	m.handleMouse(tea.MouseClickMsg{X: 1 + b.X, Y: 4 + b.Y + graph.NodeHeight/2, Button: tea.MouseLeft})
	if m.dialog == nil || m.dialog.form == nil || w.connectFrom != "" {
		t.Fatal("ports did not open mapping form")
	}

}
