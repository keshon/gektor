package agent

import (
	"errors"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/screen"
)

func TestSessionCopyNavigation(t *testing.T) {
	m := sizeModelTo(t, testModel(), 160, 30)
	m.stateFile = filepath.Join(t.TempDir(), "106db964", "state.json")
	m.sessionTitle = "Download music"
	m.state = stRunning
	m.handleMouse(tea.MouseClickMsg{X: m.termW - 3, Y: 0, Button: tea.MouseLeft})
	if m.chrome.Header.Focused || m.chrome.Header.Menu != "" {
		t.Fatal("session label opened a menu")
	}
	m.chrome.Header = screen.MenuState{Focused: true, Global: true, Menu: "session"}
	items := m.sessionCopyItems()
	for _, item := range items {
		if !item.Enabled {
			t.Fatal("copy is disabled during a run")
		}
		found := false
		for _, row := range m.menuItems("session") {
			if row.Command == item.Command && row.Enabled {
				found = true
			}
		}
		if !found {
			t.Fatal("Session menu lacks keyboard access to", item.Command)
		}
	}
	m.headerKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.chrome.Header.Focused || m.chrome.Header.Menu != "" {
		t.Fatal("ESC did not dismiss Session menu")
	}
	m.stateFile = ""
	if m.copySession("copy-session-id") != nil {
		t.Fatal("unsaved session produced a copy command")
	}
	for _, row := range m.sessionCopyItems() {
		if row.Enabled {
			t.Fatal("unsaved session has an enabled copy action")
		}
	}
}

func TestSessionCopyValuesAndClipboardFailures(t *testing.T) {
	m := testModel()
	dir := filepath.Join(t.TempDir(), "space in path", "106db964")
	m.stateFile = filepath.Join(dir, "state.json")
	_, id := m.sessionCopyValue("copy-session-id")
	label, path := m.sessionCopyValue("copy-session-path")
	if id != "106db964" || path != dir || !filepath.IsAbs(path) {
		t.Fatal("incorrect copy values", id, path)
	}
	for _, failure := range []error{nil, errors.New("clipboard unavailable")} {
		msg := copySessionCommand(label, path, func(text string) error {
			if text != dir {
				t.Fatal("clipboard received incorrect value", text)
			}
			return failure
		})().(sessionCopiedMsg)
		if msg.err != failure || msg.text != dir {
			t.Fatal("clipboard result was lost")
		}
	}
}
