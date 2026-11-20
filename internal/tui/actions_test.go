package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestActionsOpenAndDispatchThroughBothScreens(t *testing.T) {
	m := settingsModel(t)
	for _, workflow := range []bool{false, true} {
		if workflow {
			m.openWorkflows()
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyF12})
		dialog := m.chrome.DialogInfo()
		dialogID := m.chrome.DialogID()
		if dialog.Kind == screen.NoDialog || m.chrome.DialogID() != m.actionsDialog || dialog.Kind != screen.TableDialog {
			t.Fatal("Actions did not open")
		}
		m.Update(tea.PasteMsg{Content: "Appearance"})
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.chrome.DialogInfo().Kind == screen.NoDialog || m.chrome.DialogInfo().Kind != screen.FormDialog || m.chrome.DialogInfo().Title != "Appearance" {
			t.Fatal("selected action did not open Appearance")
		}
		if dialogID == m.chrome.DialogID() {
			t.Fatal("Appearance reused the Actions dialog lifetime")
		}
		m.dispatchControl(screen.Event{Kind: screen.TableAction, ID: dialogID, Action: "details", Row: "quit"})
		if m.quit || m.pendingQuit {
			t.Fatal("stale Actions result reached the application")
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	}
}
