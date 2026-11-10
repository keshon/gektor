package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Locate controls in the rendered screen, independently of the form's hit map.
func clickWorkflowText(t *testing.T, m *model, text string) {
	t.Helper()
	for y, row := range strings.Split(ansi.Strip(m.screenContent()), "\n") {
		if y < paneTop {
			continue
		} // Ignore the underlying inspector's close control.
		if pos := strings.Index(row, text); pos >= 0 {
			x := ansi.StringWidth(row[:pos])
			m.handleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			return
		}
	}
	t.Fatalf("control %q not rendered", text)
}

func TestWorkflowFormButtonsMatchRenderedScreen(t *testing.T) {
	for _, width := range []int{40, 80, 121} {
		for _, inspect := range []bool{false, true} {
			m := workflowTestModel(t)
			sizeModelTo(t, m, width, 30)
			if inspect {
				m.workflowCommand("inspector")
			}
			m.workflowCommand("open")
			clickWorkflowText(t, m, "Cancel")
			if m.dialog != nil {
				t.Fatalf("cancel missed at width %d inspector %v", width, inspect)
			}
			m.workflowCommand("open")
			m.dialog.form.Editor.SetValue("recipe.json")
			clickWorkflowText(t, m, "Continue")
			if m.dialog != nil || len(m.workflows.TakeCommands()) == 0 {
				t.Fatalf("continue missed at width %d inspector %v", width, inspect)
			}
			m.workflowCommand("open")
			clickWorkflowText(t, m, "×")
			if m.dialog != nil {
				t.Fatalf("close missed width %d inspector %v\n%s", width, inspect, ansi.Strip(m.screenContent()))
			}
		}
	}
}
