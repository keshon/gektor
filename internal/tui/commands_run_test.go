package tui

import (
	"github.com/keshon/gektor/internal/run"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A command either acts on the run or it does not, and the registry is the
// only place that can say which. Anything not marked immediate queues, so a
// new command is safe by default.

func TestCommandRegistryDeclaresWhatRunsDuringARun(t *testing.T) {
	for _, c := range commands() {
		if c.immediate {
			continue
		}
		// These touch the run, the connected backend, or the saved
		// conversation. Running one mid-turn would either restart the
		// agent under it or rewrite a file the run is still writing.
		switch c.name {
		case "/new", "/mode", "/max-steps",
			"/rename", "/delete", "/compact", "/retry":
		default:
			t.Errorf("%s queues during a run but touches nothing in it", c.name)
		}
	}
}

// The bug this exists for: a command with an argument needs two Enters, and
// the second one has no picker left to run it, so it fell through to the
// queue. /theme is the one such command that acts on nothing in the run, and
// switching theme mid-answer is exactly when a user reaches for it.
func TestCommandsWithArgumentsStillRunDuringARun(t *testing.T) {
	for _, text := range []string{"/theme dark", "/theme light"} {
		m := runningModel(t)
		m.input.SetValue(text)
		// First Enter puts the name in the composer, the second runs it.
		m.handleEnter()
		m.handleEnter()
		if m.queued != "" {
			t.Fatalf("%s queued behind the run instead of running", text)
		}
	}
}

// Anything that would restart the agent, replace the backend, or rewrite the
// saved conversation has to wait for the turn to finish.
func TestRunAffectingCommandsQueueDuringARun(t *testing.T) {
	for _, text := range []string{"/nudges full", "/mode act", "/max-steps 40", "/compact"} {
		m := runningModel(t)
		m.input.SetValue(text)
		m.handleEnter()
		m.handleEnter()
		if m.queued == "" {
			t.Fatalf("%s ran during a run; it should have queued", text)
		}
	}
}

// /stop must not queue behind the run it stops, and /quit must not either:
// queued, they were commands the user could type and nothing would happen.
func TestStopAndQuitAreNeverQueued(t *testing.T) {
	for _, text := range []string{"/stop", "/quit"} {
		m := runningModel(t)
		m.input.SetValue(text)
		m.handleEnter()
		m.handleEnter()
		if m.queued != "" {
			t.Fatalf("%s was queued while running", text)
		}
	}
}

// A view toggle has to take effect immediately, or the reader toggles it and
// watches nothing happen until the answer arrives.
func TestViewTogglesApplyImmediatelyDuringARun(t *testing.T) {
	m := runningModel(t)
	before := m.transcript.compact
	m.input.SetValue("/details")
	m.handleEnter()
	m.handleEnter()
	if m.queued != "" {
		t.Fatal("/details was queued")
	}
	if m.transcript.compact == before {
		t.Fatal("/details queued and so changed nothing")
	}
}

func runningModel(t *testing.T) *model {
	t.Helper()
	m := sizeModel(t, testModel())
	m.state = stRunning
	m.send = func(tea.Msg) {}
	return m
}

func (m *model) handleEnter() {
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
}

func TestRunningInputSubmitsSeveralMessagesWithoutWaitingForCompletion(t *testing.T) {
	m := runningModel(t)
	m.sess = &run.Session{}
	for _, text := range []string{"first update", "second update"} {
		m.input.SetValue(text)
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.input.Value() != "" || m.queued != "" {
			t.Fatal("message remained in the completion queue")
		}
	}
	if m.pendingInputCount() != 2 {
		t.Fatalf("pending=%d", m.pendingInputCount())
	}
	if !strings.Contains(m.View().Content, "Sent: 2") {
		t.Fatal("pending messages have no visible feedback")
	}
}
