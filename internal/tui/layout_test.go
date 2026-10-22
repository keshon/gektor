package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/events"
)

func TestBrowserSearchUsesWorkspace(t *testing.T) {
	m := testModel()
	m.state = stDone
	m.wsRoot = t.TempDir()
	root := sessionsRootFor(t, m.wsRoot)
	writeSessionState(t, root, "aaa", userHistory("repair login"))
	writeSessionState(t, root, "bbb", userHistory("speed up search"))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.openSessions()
	finishSessionRefresh(t, m)
	m.sessionsKey(tea.KeyPressMsg{Text: string("repair")})
	if len(m.sessions.entries) != 1 || m.sessions.entries[0].id != "aaa" {
		t.Fatal("search did not filter workspace sessions")
	}
	if m.sessions.mode != sessList {
		t.Fatal("typing r accidentally renamed")
	}
	m.sessionsKey(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.sessions.mode != sessRename {
		t.Fatal("rename shortcut failed")
	}
	m.sessionsKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.sessionsKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.sessions != nil {
		t.Fatal("browser did not close")
	}
}

func TestNavigatorCannotSwitchDuringRun(t *testing.T) {
	m := testModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.savedSessions = []sessionEntry{{id: "aaa", title: "Saved task", dir: "aaa"}}
	m.stateFile = "active/state.json"
	m.openSessions()
	m.sessions.entries = m.savedSessions
	m.sessionsKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.stateFile != "active/state.json" || m.state != stRunning {
		t.Fatal("browse interrupted active run")
	}
	// A gate arriving during browsing takes over both rendering and keys.
	m.openSessions()
	finishSessionRefresh(t, m)
	m.handleEvent(events.Event{Name: "awaiting_input", Fields: map[string]any{"kind": "permission", "prompt": "Run tests?"}})
	if m.sessions != nil || m.state != stPermission {
		t.Fatal("permission gate did not take over")
	}
}

func TestFramesFitTerminalCells(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {110, 24}, {80, 24}, {40, 18}, {20, 10}} {
		for _, browser := range []bool{false, true} {
			m := testModel()
			m.state = stDone
			m.wsRoot = t.TempDir()
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.appendBlock(userBlock(strings.Repeat("Wide 界 🚀 text ", 20)))
			m.input.SetValue("first line\nsecond line")
			m.fitInput()
			m.fitBottom()
			if browser {
				m.openSessions()
				finishSessionRefresh(t, m)
			}
			lines := strings.Split(m.View().Content, "\n")
			if len(lines) != size[1] {
				t.Fatalf("size %v: %d rows", size, len(lines))
			}
			for _, line := range lines {
				if lipgloss.Width(line) > size[0] {
					t.Fatalf("size %v: overflowing line %q", size, line)
				}
			}
		}
	}
}

func TestReflowPreservesWideCharacters(t *testing.T) {
	input := strings.Repeat("界🚀é", 20)
	lines := reflow(input, 12)
	if strings.Join(lines, "") != input {
		t.Fatal("wrapping lost text or split a grapheme")
	}
	for _, line := range lines {
		if lipgloss.Width(line) > 12 {
			t.Fatalf("wide characters exceeded pane: %q", line)
		}
	}
}

func TestConversationComposerWrapsWithinPane(t *testing.T) {
	m := completionModel(t)
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	m.Update(tea.PasteMsg{Content: strings.Repeat("text ", 30)})
	if m.input.Height() < 2 {
		t.Fatal("draft wrapped against terminal width instead of Identity width")
	}
	for _, state := range []runState{stDone, stRunning, stAsk, stPermission} {
		m.state = state
		m.fitBottom()
		view := m.View()
		if lipgloss.Height(view.Content) != m.termH {
			t.Fatalf("state %d changed frame height", state)
		}
		if view.Cursor != nil && view.Cursor.X < m.promptOffset() {
			t.Fatalf("state %d placed caret in Sessions", state)
		}
	}
}

func TestNestedComposerCompletesFilesAndCommands(t *testing.T) {
	m := completionModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	writeReference(t, m, "notes.txt", []byte("workspace notes"))
	m.Update(tea.PasteMsg{Content: "first line"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	m.Update(tea.PasteMsg{Content: "read @no"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.input.Value() != "first line\nread @notes.txt " || m.input.Height() != 2 || m.state != stDone {
		t.Fatal("file completion submitted or lost the multiline draft")
	}
	view := m.View()
	if view.Cursor == nil || view.Cursor.X < m.promptOffset() || view.Cursor.Y != m.termH-m.footerRows()-1 {
		t.Fatalf("file completion misplaced the caret: %+v", view.Cursor)
	}
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m.Update(tea.PasteMsg{Content: "/hel"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog == nil || !m.dialog.help || m.input.Value() != "" {
		t.Fatal("command completion did not open help")
	}
	m.closeDialog()
	m.state = stPermission
	m.gstage = gsReject
	m.input.Blur()
	m.note.Focus()
	m.note.SetValue("use another command")
	m.fitBottom()
	view = m.View()
	if view.Cursor == nil || view.Cursor.X < m.promptOffset() || view.Cursor.Y != m.termH-m.footerRows()-1 {
		t.Fatalf("permission note misplaced the caret: %+v", view.Cursor)
	}
}
