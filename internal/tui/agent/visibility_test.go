package agent

import (
	"testing"

	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestHiddenAgentResultsCannotReplaceApplicationOverlay(t *testing.T) {
	for _, result := range []any{
		sessionCopiedMsg{label: "Session ID", text: "original"},
		processListMsg{view: screen.TableID{}},
		mcpDoneMsg{},
	} {
		m := sizeModel(t, testModel())
		m.state = stDone
		m.input.SetValue("retained draft")
		m.Hide()
		m.Update(result)
		if m.chrome.DialogInfo().Kind != 0 || m.input.Focused() || m.input.Value() != "retained draft" {
			t.Fatalf("hidden result %T took control of the application", result)
		}
		m.Activate()
		if !m.input.Focused() || m.input.Value() != "retained draft" {
			t.Fatal("returning to Agent lost its input ownership")
		}
	}
}
