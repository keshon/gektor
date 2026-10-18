package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The agent rewrites its snapshot on every step, so the active session is a
// permanent cache miss in the session index. Scanning at the start of a turn
// as well as the end therefore decoded the same freshly-written file twice
// per turn, for a store that cannot have changed in between: nothing
// creates, renames or deletes a session directory while a turn is running.

func TestStartingATurnDoesNotRescanTheSessionStore(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.wsRoot = t.TempDir()
	// startRun's worker reports completion through send; a unit model has
	// no program to deliver to.
	m.send = func(tea.Msg) {}
	m.refreshNavigator()
	if cmd := m.takeCommands(); cmd == nil {
		t.Fatal("launch did not queue a scan")
	}
	before := m.sessionRevision

	m.startRun(func(ctx context.Context) (string, error) { return "", nil })
	if cmd := m.takeCommands(); cmd != nil {
		t.Fatal("turn start queued another session-store scan")
	}
	if m.sessionRevision != before {
		t.Fatal("turn start invalidated the session index revision")
	}
	if m.state != stRunning {
		t.Fatalf("turn did not start: state = %v", m.state)
	}
}

// The scan that ends a turn is the one that matters: the file changed.
func TestFinishingATurnStillRescansTheSessionStore(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	m.wsRoot = t.TempDir()
	m.refreshNavigator()
	m.takeCommands()
	before := m.sessionRevision

	updated, _ := m.Update(doneMsg{})
	m = updated.(*model)
	if m.sessionRevision == before {
		t.Fatal("finishing a turn did not refresh the session index")
	}
}
