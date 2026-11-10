package tui

import (
	"errors"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSessionCopyNavigation(t *testing.T) {
	m := sizeModelTo(t, testModel(), 160, 30)
	m.stateFile = filepath.Join(t.TempDir(), "106db964", "state.json")
	m.sessionTitle = "Download music"
	m.state = stRunning
	b := m.sessionIdentityBadge()
	m.handleMouse(tea.MouseClickMsg{X: b.start, Y: 0, Button: tea.MouseLeft})
	if m.header.menu != "session-info" || !m.header.global {
		t.Fatal("session label did not open its menu during a run")
	}
	items := m.headerItems()
	if len(items) != 2 || items[0].command != "copy-session-id" || items[1].command != "copy-session-path" {
		t.Fatal("copy menu actions differ", items)
	}
	for _, item := range items {
		if !item.enabled {
			t.Fatal("copy is disabled during a run")
		}
		found := false
		for _, row := range m.menuItems("session") {
			if row.command == item.command && row.enabled {
				found = true
			}
		}
		if !found {
			t.Fatal("Session menu lacks keyboard access to", item.command)
		}
	}
	l := m.headerMenuLayout()
	if l.X < 0 || l.X+l.Width > m.termW {
		t.Fatal("right-aligned popup escaped terminal")
	}
	m.headerKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.header.focused || m.header.menu != "" {
		t.Fatal("ESC did not dismiss copy menu")
	}
	m.width(tea.WindowSizeMsg{Width: 40, Height: 30})
	if m.sessionIdentityBadge().id != "" {
		t.Fatal("hidden session identity retained a hit target")
	}
	m.stateFile = ""
	if m.copySession("copy-session-id") != nil {
		t.Fatal("unsaved session produced a copy command")
	}
	for _, row := range m.sessionCopyItems() {
		if row.enabled {
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
