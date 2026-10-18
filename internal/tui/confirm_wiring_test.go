package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/run"
)

// The confirm component was built, tested in isolation, and then never
// wired to a render path: commands pushed an overlay that drew nothing,
// so a user pressing y restarted a session without ever being asked.
// These cover the wiring, not the component.

func TestRestartCommandOpensAVisibleConfirmation(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.sess = &run.Session{}

	m.command("/mode plan")
	if m.dialog == nil || m.dialog.confirm == nil {
		t.Fatal("no confirmation dialog is open")
	}

	view := m.screenContent()
	for _, want := range []string{"Restart session?", "restart", "cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("confirmation is not visible on screen; %q missing from:\n%s", want, view)
		}
	}
}

func TestConfirmationAcceptRunsThePendingAction(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.sess = &run.Session{}

	ran := false
	m.confirmAction = func() { ran = true }
	m.openConfirm("Restart session?", "This command changes agent settings.", "restart", "cancel", "body")

	m.handleKey(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !ran {
		t.Fatal("y did not run the pending action")
	}
	if m.confirmAction != nil {
		t.Fatal("pending action survived the confirmation")
	}
	if m.dialog != nil {
		t.Fatal("confirmation dialog stayed open after accepting")
	}
}

func TestConfirmationCancelDropsThePendingAction(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		{Code: 'n', Text: "n"},
		{Code: tea.KeyEsc},
	} {
		m := sizeModel(t, testModel())
		m.state = stDone
		m.sess = &run.Session{}

		ran := false
		m.confirmAction = func() { ran = true }
		m.openConfirm("Restart session?", "sub", "restart", "cancel", "body")

		m.handleKey(key)
		if ran {
			t.Fatalf("%v ran the pending action", key)
		}
		if m.confirmAction != nil {
			t.Fatalf("%v left the pending action armed", key)
		}
		if m.dialog != nil {
			t.Fatalf("%v left the confirmation open", key)
		}
	}
}

// A confirmation is a takeover: a stray letter must not reach the composer
// behind it, or "y" would both answer the prompt and type into the draft.
func TestConfirmationSwallowsTyping(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.sess = &run.Session{}

	ran := false
	m.confirmAction = func() { ran = true }
	m.openConfirm("Restart session?", "sub", "restart", "cancel", "body")

	m.handleKey(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if ran {
		t.Fatal("an unrelated key answered the confirmation")
	}
	if m.input.Value() != "" {
		t.Fatalf("a letter reached the composer: %q", m.input.Value())
	}
	if m.dialog == nil || m.dialog.confirm == nil {
		t.Fatal("confirmation closed on an unrelated key")
	}
}

// Cancelling must give focus back, or the composer is dead after one Esc.
func TestConfirmationRestoresComposerFocus(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.sess = &run.Session{}
	m.openConfirm("Restart session?", "sub", "restart", "cancel")

	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !m.input.Focused() {
		t.Fatal("composer did not regain focus after cancelling")
	}
}
