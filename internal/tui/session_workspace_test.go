package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/run"
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

func TestWorkspaceReassignmentStaysPinnedAcrossRefresh(t *testing.T) {
	for _, changed := range []string{"removed", "reordered", "workspace"} {
		t.Run(changed, func(t *testing.T) {
			m := settingsModel(t)
			root := t.TempDir()
			a := writeSessionState(t, root, "a", userHistory("A"))
			b := writeSessionState(t, root, "b", userHistory("B"))
			m.sessions = &sessionsState{filter: newSearchInput(30), entries: []sessionEntry{{dir: a}, {dir: b}}}
			m.sessionsKey(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
			switch changed {
			case "removed":
				m.sessions.replaceEntries([]sessionEntry{{dir: b}})
			case "reordered":
				m.sessions.replaceEntries([]sessionEntry{{dir: b}, {dir: a}})
			case "workspace":
				assignSavedWorkspace(t, filepath.Join(a, "state.json"), t.TempDir())
			}
			m.sessionsKey(keyRunes('y'))
			saved, err := session.Load(filepath.Join(b, "state.json"))
			if err != nil || saved.Workspace != "" {
				t.Fatalf("confirmation reassigned B: %+v %v", saved, err)
			}
			if changed == "reordered" {
				saved, err = session.Load(filepath.Join(a, "state.json"))
				if err != nil || saved.Workspace != m.wsRoot {
					t.Fatal("did not reassign pinned session", err)
				}
			} else if m.sessions.flash == "" {
				t.Fatal("stale confirmation was not explained")
			}
		})
	}
}

func TestWorkspaceSwitchKeepsSourceAndTargetDrafts(t *testing.T) {
	m := settingsModel(t)
	source := m.stateFile
	m.input.SetValue("source draft")
	m.draftImages = []string{"source.png"}
	m.queued, m.queuedImages = "queued", []string{"queued.png"}
	target := writeSessionState(t, t.TempDir(), "target", userHistory("target"))
	assignSavedWorkspace(t, filepath.Join(target, "state.json"), t.TempDir())
	m.drafts = Drafts{filepath.Join(target, "state.json"): {text: "target draft", images: []string{"target.png"}}}
	m.sessions = &sessionsState{entries: []sessionEntry{{dir: target}}}
	if m.openSelected() == nil {
		t.Fatal("no relaunch")
	}
	next := settingsModel(t)
	next.drafts = m.modeChange.Drafts
	next.stateFile = filepath.Join(target, "state.json")
	next.restoreDraft()
	if next.input.Value() != "target draft" || next.draftImages[0] != "target.png" {
		t.Fatal("source draft leaked into target")
	}
	next.stateFile = source
	next.restoreDraft()
	if next.input.Value() != "source draft" || next.draftImages[0] != "source.png" || next.queued != "queued" || next.queuedImages[0] != "queued.png" {
		t.Fatal("source draft lost")
	}
}

func TestSessionRecoveryAtNormalAndNarrowWidths(t *testing.T) {
	for _, width := range []int{40, 80} {
		m := sizeModelTo(t, testModel(), width, 24)
		m.state = stDone
		m.wsRoot = `D:\Projects\a-very-long-path\nested\actual-target`
		m.openSessions()
		m.sessions.loading = false
		m.sessions.entries = []sessionEntry{{title: "Legacy session", dir: t.TempDir()}}
		plain := ansi.Strip(m.searchView())
		for _, want := range []string{"ESC close", "ENTER open", "↑↓"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("width %d missing %q:\n%s", width, want, plain)
			}
		}
		m.sessionsKey(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
		plain = ansi.Strip(m.searchView())
		for _, want := range []string{"actual-target", "Y assign", "ESC back"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("width %d missing %q:\n%s", width, want, plain)
			}
		}
		for _, row := range strings.Split(plain, "\n") {
			if ansi.StringWidth(row) > width {
				t.Fatalf("row exceeds %d: %s", width, row)
			}
		}
		m.sessionsKey(keyRunes('n'))
		if m.sessions.mode != sessList || m.sessions.reassign != nil {
			t.Fatal("cancel kept assignment")
		}
	}
}

func TestUnavailableWorkspaceRecoveryDoesNotFallback(t *testing.T) {
	m := settingsModel(t)
	root := t.TempDir()
	dir := writeSessionState(t, t.TempDir(), "missing", userHistory("saved"))
	assignSavedWorkspace(t, filepath.Join(dir, "state.json"), root)
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	m.sessions = &sessionsState{entries: []sessionEntry{{dir: dir}}}
	if m.openSelected() != nil || m.modeChange != nil || !strings.Contains(m.sessions.flash, "CTRL+W") {
		t.Fatal("missing workspace fell back")
	}
}

func TestExplicitNewTaskAfterRelaunchDoesNotReuseSavedSession(t *testing.T) {
	m := settingsModel(t)
	source := m.stateFile
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	m.sess = nil
	m.seedHistory = userHistory("old conversation")
	var destination string
	m.newSession = func(task, path string, images []string) (*run.Session, error) {
		destination = path
		return nil, errors.New("stop before model")
	}
	m.command("/new different task")
	if destination == "" || destination == source || len(m.seedHistory) != 0 {
		t.Fatalf("new task reused old session: %q", destination)
	}
	after, err := os.ReadFile(source)
	if err != nil || string(after) != string(before) {
		t.Fatal("saved session changed", err)
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
	if cmd := m.openSelected(); cmd != nil || m.modeChange != nil || !strings.Contains(m.sessions.flash, "CTRL+W") {
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
