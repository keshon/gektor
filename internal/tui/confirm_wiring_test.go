package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/run"
)

// A session-restarting command used to push an overlay that drew nothing,
// so pressing y restarted a session without the user ever being asked.
// These cover the confirmation itself, which is the same destructive form
// the delete prompt uses: no fields, the choice lands on Cancel, and Enter
// acts only on what is selected.

func restartModel(t *testing.T) *model {
	t.Helper()
	m := sizeModel(t, testModel())
	m.state = stDone
	m.sess = &run.Session{}
	return m
}

func TestRestartCommandOpensADestructiveConfirmation(t *testing.T) {
	m := restartModel(t)
	m.command("/mode plan")
	if m.dialog == nil || m.dialog.form == nil {
		t.Fatal("no confirmation is open")
	}
	if !m.dialog.form.Destructive {
		t.Fatal("a restart confirmation is not marked destructive")
	}
	if len(m.dialog.form.Fields) != 0 {
		t.Fatalf("confirmation asks for input: %+v", m.dialog.form.Fields)
	}
	// It must open on Cancel. A confirmation that starts on Restart is one
	// stray Enter away from a restart the user never chose.
	if got := m.dialog.form.Index; got != 1 {
		t.Fatalf("focus starts at %d, want Cancel (1)", got)
	}

	view := m.screenContent()
	want := []string{"Restart session", "[ Restart ]", "[ Cancel ]"}
	for _, w := range want {
		if !strings.Contains(view, w) {
			t.Fatalf("%q missing from the confirmation:\n%s", w, view)
		}
	}
}

func TestConfirmationEnterOnCancelDoesNotRestart(t *testing.T) {
	m := restartModel(t)
	ran := false
	m.confirmRestart(func() { ran = true })

	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if ran {
		t.Fatal("Enter on Cancel ran the restart")
	}
	if m.dialog != nil {
		t.Fatal("the confirmation stayed open after cancelling")
	}
}

func TestConfirmationSelectsThenConfirms(t *testing.T) {
	m := restartModel(t)
	ran := false
	m.confirmRestart(func() { ran = true })

	// Move from Cancel to Restart, then commit.
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.dialog == nil || m.dialog.form.Index != 0 {
		t.Fatal("did not move onto the Restart button")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !ran {
		t.Fatal("Enter on Restart did not run it")
	}
	if m.dialog != nil {
		t.Fatal("the confirmation stayed open after confirming")
	}
}

func TestConfirmationEscapeCancelsWithoutRunning(t *testing.T) {
	m := restartModel(t)
	ran := false
	m.confirmRestart(func() { ran = true })

	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if ran {
		t.Fatal("Esc ran the restart")
	}
	if m.dialog != nil {
		t.Fatal("Esc left the confirmation open")
	}
	if !m.input.Focused() {
		t.Fatal("composer did not regain focus after cancelling")
	}
}

// A confirmation is a takeover: a stray letter must not reach the composer
// behind it, or "y" would both answer the prompt and type into the draft.
func TestConfirmationSwallowsTyping(t *testing.T) {
	m := restartModel(t)
	ran := false
	m.confirmRestart(func() { ran = true })

	m.handleKey(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if ran {
		t.Fatal("an unrelated key answered the confirmation")
	}
	if m.input.Value() != "" {
		t.Fatalf("a letter reached the composer: %q", m.input.Value())
	}
	if m.dialog == nil {
		t.Fatal("confirmation closed on an unrelated key")
	}
}

// A centred dialog is one frame, not two. Indenting only the first line of
// the box shifted the top border away from the body it belongs to, which
// reads as an empty box floating beside the real one.
func TestConfirmationRendersAsOneAlignedFrame(t *testing.T) {
	m := restartModel(t)
	m.confirmRestart(func() {})

	lines := strings.Split(m.dialogView(), "\n")
	var box []string
	for _, l := range lines {
		if strings.Contains(ansi.Strip(l), "╭") || strings.Contains(ansi.Strip(l), "│") || strings.Contains(ansi.Strip(l), "╰") {
			box = append(box, l)
		}
	}
	if len(box) < 4 {
		t.Fatalf("no framed dialog in %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	width := lipgloss.Width(ansi.Strip(box[0]))
	for i, l := range box {
		if got := lipgloss.Width(ansi.Strip(l)); got != width {
			t.Fatalf("dialog line %d is %d cells, want %d — the frame is ragged:\n%s",
				i, got, width, strings.Join(box, "\n"))
		}
	}
	if !strings.HasPrefix(ansi.Strip(box[0]), strings.Repeat(" ", 1)) {
		t.Fatal("dialog is flush against the left edge")
	}
	if !strings.Contains(strings.Join(box, "\n"), "Restart session") {
		t.Fatalf("the dialog does not show what it is asking:\n%s", strings.Join(box, "\n"))
	}
}

// No session means nothing to restart, so the command runs outright rather
// than showing a prompt the user cannot act on.
func TestNoSessionRunsWithoutPrompting(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	ran := false
	m.confirmRestart(func() { ran = true })

	if !ran {
		t.Fatal("action did not run")
	}
	if m.dialog != nil {
		t.Fatal("a prompt was shown with no session to restart")
	}
}
