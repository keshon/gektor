package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/session"
)

func TestWorkspaceFilterKeepsGlobalEntries(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	s := sessionsState{currentWorkspace: root, filter: newSearchInput(30), all: []sessionEntry{{id: "a", workspace: root}, {id: "b", workspace: other}, {id: "legacy"}}}
	s.currentOnly = true
	s.applyFilter()
	if len(s.entries) != 1 || s.entries[0].id != "a" || len(s.all) != 3 {
		t.Fatalf("current filter: %+v", s.entries)
	}
	s.currentOnly = false
	s.applyFilter()
	if len(s.entries) != 3 {
		t.Fatal("global list lost")
	}
}

func TestOpenSessionRestoresWorkspaceOrReportsMissing(t *testing.T) {
	m := settingsModel(t)
	root := t.TempDir()
	snapshot := session.New(userHistory("saved task"))
	snapshot.Workspace = root
	dir := t.TempDir()
	if err := snapshot.Save(filepath.Join(dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	m.application = &Application{Preferences: config.Preferences{Workspace: m.wsRoot}}
	m.sessions = &sessionsState{entries: []sessionEntry{{dir: dir, title: "saved"}}}
	if cmd := m.openSelected(); cmd == nil {
		t.Fatal("no workspace relaunch")
	}
	if m.modeChange == nil || m.modeChange.Preferences.Workspace != root || m.modeChange.Directory != dir {
		t.Fatalf("wrong relaunch: %+v", m.modeChange)
	}
	m.modeChange = nil
	snapshot.Workspace = filepath.Join(root, "missing")
	if err := snapshot.Save(filepath.Join(dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	if cmd := m.openSelected(); cmd != nil || m.modeChange != nil || !strings.Contains(m.sessions.flash, "Ctrl+W") {
		t.Fatal("missing workspace silently restored")
	}
}

func TestWorkspaceReassignmentRequiresConfirmation(t *testing.T) {
	m := settingsModel(t)
	old := t.TempDir()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	snapshot := session.New(userHistory("saved"))
	snapshot.Workspace = old
	if err := snapshot.Save(path); err != nil {
		t.Fatal(err)
	}
	m.sessions = &sessionsState{entries: []sessionEntry{{dir: dir, workspace: old}}}
	check := func(want string) {
		t.Helper()
		saved, err := session.Load(path)
		if err != nil || saved.Workspace != want || len(saved.History) != len(snapshot.History) {
			t.Fatalf("saved session: %+v, %v; want workspace %q", saved, err, want)
		}
	}
	m.sessionsKey(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if m.sessions.mode != sessReassign {
		t.Fatal("reassignment skipped confirmation")
	}
	check(old)
	m.sessionsKey(keyRunes('n'))
	check(old)
	m.sessionsKey(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	m.sessionsKey(keyRunes('y'))
	check(m.wsRoot)
}

func TestLegacySessionDoesNotOpenInCurrentWorkspace(t *testing.T) {
	m := settingsModel(t)
	dir := writeSessionState(t, t.TempDir(), "legacy", userHistory("old"))
	m.sessions = &sessionsState{entries: []sessionEntry{{dir: dir}}}
	if cmd := m.openSelected(); cmd != nil || !strings.Contains(m.sessions.flash, "unassigned") {
		t.Fatal("legacy session used current workspace without selection")
	}
}

// Fixtures that exercise opening must opt in to a real workspace; legacy
// fixtures remain unassigned so migration behavior stays testable.
func assignSavedWorkspace(t *testing.T, path, root string) {
	t.Helper()
	snapshot, err := session.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Workspace = root
	if err := snapshot.Save(path); err != nil {
		t.Fatal(err)
	}
}
