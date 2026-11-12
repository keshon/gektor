package agent

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSessionToolbarClickReturnsToExistingDraft(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("unfinished message")
	m.openSessions()
	for i, action := range m.footerActions() {
		if action.Key == 'p' {
			hit := m.testControls().ToolbarLayout().Cells[i].Bounds
			m.handleMouse(tea.MouseClickMsg{X: hit.X, Y: m.termH - 1, Button: tea.MouseLeft})
			if m.sessions != nil || m.input.Value() != "unfinished message" || !m.input.Focused() {
				t.Fatal("session toolbar ignored its action or lost the draft")
			}
			return
		}
	}
	t.Fatal("session search toolbar action missing")
}

func TestFormToolbarUsesTheFormsPrimaryAction(t *testing.T) {
	m := sizeModel(t, testModel())
	m.testControls().OpenForm("Open recipe", "", "Open", nil)
	for _, action := range m.footerActions() {
		if action.Key == 's' {
			if action.Label != "Open" || !action.Enabled {
				t.Fatal("toolbar disagrees with the form action")
			}
			return
		}
	}
	t.Fatal("form toolbar primary action missing")
}
