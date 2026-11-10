package workflow

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

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
