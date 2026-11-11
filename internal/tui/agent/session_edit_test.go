package agent

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDirectRenameAndDeleteDefaultCancel(t *testing.T) {
	m := settingsModel(t)
	path := m.stateFile
	m.renameActive()
	m.chrome.Dialog.Form.Editor.SetValue("New title")
	m.testKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.chrome.Dialog != nil || m.sessionTitle != "New title" {
		t.Fatal("rename failed")
	}
	m.deleteActive()
	m.testKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.chrome.Dialog == nil {
		t.Fatal("delete bypassed default cancel")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, err := os.Stat(path); err != nil {
		t.Fatal("default action deleted Identity")
	}
	m.deleteActive()
	m.testKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, err := os.Stat(path); !os.IsNotExist(err) || m.sess != nil {
		t.Fatal("explicit delete failed")
	}
}
