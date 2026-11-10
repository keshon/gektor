package agent

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMultilinePasteWaitsForEnterInConversationStates(t *testing.T) {
	text := "first line\n/quit\n/new another task\nUnicode: Привет 😀\nlast line\n"
	for _, state := range []runState{stDone, stRunning, stAsk} {
		m := sizeModel(t, testModel())
		m.state = state
		m.Update(tea.PasteStartMsg{})
		m.Update(tea.PasteMsg{Content: text})
		m.Update(tea.PasteEndMsg{})
		if m.input.Value() != text || m.state != state || m.queued != "" || m.quit {
			t.Fatalf("paste changed task in state %d: %q", state, m.input.Value())
		}
		if state == stRunning {
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if m.input.Value() != "" || m.queued != strings.TrimSpace(text) {
				t.Fatal("Enter did not submit pasted draft")
			}
		}
	}
}
