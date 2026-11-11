package agent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/tui/settings"
)

func TestSessionLibrariesAreIndependentAndCapturedAtConstruction(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	writeSessionState(t, firstRoot, "first", userHistory("first library"))
	writeSessionState(t, secondRoot, "second", userHistory("second library"))

	profile := &app.PreparedProfile{Directory: firstRoot}
	first, err := New(context.Background(), Config{
		Application:     &settings.Application{Temporary: true},
		PreparedProfile: profile,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := New(context.Background(), Config{SessionsRoot: secondRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	profile.Directory = secondRoot
	first.openSessions()
	second.openSessions()
	finishSessionRefresh(t, first)
	finishSessionRefresh(t, second)
	if len(first.sessions.entries) != 1 || first.sessions.entries[0].id != "first" {
		t.Fatal("first browser used another library")
	}
	if len(second.sessions.entries) != 1 || second.sessions.entries[0].id != "second" {
		t.Fatal("second browser used another library")
	}
	if first.taskDirectory("new") != filepath.Join(firstRoot, "new") {
		t.Fatal("new sessions do not use the browser's captured library")
	}
}

func TestSessionRenameAndPermissionInputsAreIndependent(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.sessionsRoot = t.TempDir()
	writeSessionState(t, m.sessionsRoot, "one", userHistory("original"))
	m.note.SetValue("permission draft")
	m.openSessions()
	finishSessionRefresh(t, m)
	m.testUpdate(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m.testUpdate(tea.PasteMsg{Content: "new title"})
	if m.sessions.rename.Value() != "new title" || m.note.Value() != "permission draft" {
		t.Fatal("rename input shares state with the permission input")
	}
	if m.activeInputOwner() != focusSessionRename || m.note.Focused() {
		t.Fatal("rename does not have exclusive input ownership")
	}

	m.state = stPermission
	m.gstage = gsReject
	m.syncFocus()
	m.testUpdate(tea.PasteMsg{Content: " changed"})
	if m.sessions.rename.Value() != "new title" || !strings.Contains(m.note.Value(), " changed") {
		t.Fatal("permission input reached the suspended rename")
	}
	if m.sessions.rename.Focused() || m.activeInputOwner() != focusNote {
		t.Fatal("suspended rename retained input ownership")
	}
	caret := m.View().Cursor
	if caret == nil || caret.Y != m.termH-m.footerRows()-1 {
		t.Fatal("permission caret was placed in the suspended browser")
	}

	m.state = stDone
	m.closeSessions()
	if m.note.Value() == "" || m.note.Prompt != "note> " || m.note.CharLimit != 240 {
		t.Fatal("closing the browser changed the permission draft or configuration")
	}
}

func TestFailedSessionSwitchRetainsCurrentAgentState(t *testing.T) {
	m := settingsModel(t)
	oldSession := m.sess
	oldStateFile := m.stateFile
	oldBlocks := len(m.transcript.blocks)
	oldDraft := m.input.Value()
	dir := writeSessionState(t, m.sessionRoot(), "other", userHistory("other task"))
	assignSavedWorkspace(t, filepath.Join(dir, "state.json"), m.wsRoot)
	m.openSessions()
	finishSessionRefresh(t, m)
	for i, entry := range m.sessions.entries {
		if entry.dir == dir {
			m.sessions.Cursor = i
		}
	}
	m.newSession = func(string, string, []string) (*harness.Session, error) {
		return nil, errors.New("cannot construct session")
	}

	cmd := m.openSelected()
	if cmd != nil || m.sessions == nil || !strings.Contains(m.sessions.flash, "cannot construct session") {
		t.Fatal("failed switch did not remain in the browser with an error")
	}
	if m.sess != oldSession || m.stateFile != oldStateFile || m.input.Value() != oldDraft || len(m.transcript.blocks) != oldBlocks {
		t.Fatal("failed switch replaced the current session, draft, or transcript")
	}
}
