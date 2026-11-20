package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/screen"
)

func TestActionsOpenAndDispatchThroughBothScreens(t *testing.T) {
	m := settingsModel(t)
	for _, workflow := range []bool{false, true} {
		if workflow {
			m.openWorkflows()
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyF12})
		dialog := m.chrome.Dialog
		dialogID := m.chrome.DialogID()
		if dialog == nil || m.chrome.DialogID() != m.actionsDialog || dialog.Table == nil {
			t.Fatal("Actions did not open")
		}
		m.Update(tea.PasteMsg{Content: "Appearance"})
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil || m.chrome.Dialog.Title != "Appearance" {
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
