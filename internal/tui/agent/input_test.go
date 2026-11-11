package agent

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestModifiedEnterAddsNewlineWithoutSubmitting(t *testing.T) {
	for _, state := range []runState{stDone, stAsk, stRunning} {
		for _, key := range []tea.KeyPressMsg{
			{Code: tea.KeyEnter, Mod: tea.ModShift},
			{Code: 'o', Mod: tea.ModCtrl},
		} {
			m := sizeModel(t, testModel())
			m.state = state
			for _, r := range "first" {
				m.testUpdate(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
			m.testUpdate(key)
			m.testUpdate(tea.KeyPressMsg{Code: 's', Text: "second"})
			if m.input.Value() != "first\nsecond" || m.input.Height() != 2 || m.state != state || m.queued != "" {
				t.Fatalf("%s in state %d submitted or lost multiline input: %q", key.String(), state, m.input.Value())
			}
			if cursor := m.testView().Cursor; cursor == nil || cursor.Y != m.termH-m.footerRows()-1 {
				t.Fatalf("newline scrolled out earlier rows despite room in the composer: %+v", cursor)
			}
			if state == stRunning {
				m.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
				if m.queued != "first\nsecond" || m.input.Value() != "" {
					t.Fatal("plain Enter did not queue the multiline follow-up")
				}
			}
		}
	}
}

func TestPasteGrowsComposerWithoutSubmitting(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.testUpdate(tea.PasteMsg{Content: "first\nsecond"})
	if m.input.Value() != "first\nsecond" || m.input.Height() != 2 || m.state != stDone {
		t.Fatal("paste submitted text or did not fit the composer")
	}
}

func TestWrappedInputGrowsComposer(t *testing.T) {
	m := testModel()
	m.state = stDone
	m.testUpdate(tea.WindowSizeMsg{Width: 40, Height: 24})
	m.testUpdate(tea.PasteMsg{Content: "This is a longer paragraph that should wrap across multiple visible input rows."})
	if m.input.Height() < 2 || m.testView().Cursor == nil {
		t.Fatal("wrapped text remained squeezed into a single row")
	}
}

// A trailing newline in answer text is content termination, not a blank
// line: joined with the uniform block gap it must not double.
func TestAnswerTrailingNewlineNoDoubleGap(t *testing.T) {
	st := defaultStyles()
	ans := answerBlock("I'll explore my own workspace.\n")
	tool := toolCardBlock("c1", "read_file")
	tool.result = "ok"
	tool.open = false
	joined := renderBlock(ans, st, tagged(), 80) + "\n\n" +
		renderBlock(tool, st, tagged(), 80)
	if gap := consecutiveBlanks(joined); gap > 1 {
		t.Fatalf("double gap after reply with trailing newline:\n%q", joined)
	}
	if ans.text != "I'll explore my own workspace.\n" {
		t.Fatalf("stored text mutated: %q", ans.text)
	}
}

func TestHistory_ExplicitRecallRestoresDraft(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.Push("first")
	m.input.Push("second")
	m.input.SetValue("draft\nsecond line")
	for _, want := range []string{"second", "first"} {
		m.testUpdate(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt})
		if m.input.Value() != want {
			t.Fatalf("recall %q, want %q", m.input.Value(), want)
		}
	}
	for _, want := range []string{"second", "draft\nsecond line"} {
		m.testUpdate(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt})
		if m.input.Value() != want {
			t.Fatalf("recall %q, want %q", m.input.Value(), want)
		}
	}
}

func TestHistory_ArrowsNeverRecallAtEditorEdges(t *testing.T) {
	for _, state := range []runState{stDone, stAsk, stRunning} {
		m := sizeModel(t, testModel())
		m.state = state
		m.input.Push("old")
		m.input.SetValue("first line")
		m.testUpdate(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
		m.testUpdate(keyRunes('x'))
		draft := m.input.Value()
		if draft != "first line\nx" {
			t.Fatalf("newline failed: %q", draft)
		}
		for _, direction := range []rune{tea.KeyUp, tea.KeyDown} {
			for i := 0; i < 6; i++ {
				m.testUpdate(tea.KeyPressMsg{Code: direction})
			}
			if m.input.Value() != draft || m.input.Height() < 2 {
				t.Fatalf("state %d: arrows replaced multiline draft: %q", state, m.input.Value())
			}
		}
	}
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.Push("old")
	m.input.SetValue("single line")
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.input.Value() != "single line" {
		t.Fatal("down erased idle draft")
	}
}

func TestHistory_DedupesConsecutive(t *testing.T) {
	m := sizeModel(t, testModel())
	m.input.Push("x")
	m.input.Push("x")
	m.input.Push("y")
	if len(m.input.Snapshot().History) != 2 {
		t.Fatalf("hist = %v", m.input.Snapshot().History)
	}
}

// The bubbles textarea defaults line numbers on: an empty box then
// renders a phantom "1" that looks like content but submits nothing.
func TestInputHidesLineNumbers(t *testing.T) {
	m := testModel()
	if view := ansi.Strip(m.input.View()); strings.Contains(view, "1") {
		t.Fatalf("empty input renders %q — line gutter leaking?", view)
	}
}

func TestNativeUnderlineCaretTracksComposerAndHidesOnHelp(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("first\nsecond")
	m.input.CursorEnd()
	m.fitInput()
	m.fitBottom()
	v := m.testView()
	if v.Cursor == nil || v.Cursor.Shape != tea.CursorUnderline || v.Cursor.Y != m.termH-m.footerRows()-1 {
		t.Fatalf("incorrect native composer cursor: %+v", v.Cursor)
	}
	if v.Cursor.X != 3+len("second") {
		t.Fatalf("caret did not follow text: %+v", v.Cursor)
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyF1})
	if m.testView().Cursor != nil {
		t.Fatal("help exposed background caret")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyF1})
	if m.testView().Cursor == nil || m.input.Value() != "first\nsecond" {
		t.Fatal("closing help lost caret or draft")
	}
}

func TestNewlineKey(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("a")
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	mm := updated.(*Model)
	if !strings.Contains(mm.input.Value(), "\n") {
		t.Fatalf("ctrl+o must insert newline, got %q", mm.input.Value())
	}
}

func TestQuitNeedsCtrlQ(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	// Typing a q-word never quits, first letter or not.
	for _, r := range "queen" {
		updated, _ := m.testUpdate(keyRunes(r))
		mm := updated.(*Model)
		if mm.quit {
			t.Fatalf("typing %q quit", r)
		}
		m = mm
	}
	if m.input.Value() != "queen" {
		t.Fatalf("input = %q, want the typed word", m.input.Value())
	}
	// A lone q on an empty box is still just typing.
	m.input.SetValue("")
	updated, _ := m.testUpdate(keyRunes('q'))
	if mm := updated.(*Model); mm.quit || mm.input.Value() != "q" {
		t.Fatal("q must type, never quit")
	}
	// Quit lives on ctrl+q, with or without a draft.
	updated, _ = m.testUpdate(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if mm := updated.(*Model); len(mm.TakeRequests()) != 1 {
		t.Fatal("ctrl+q must quit")
	}
}

func TestQDraftsDuringRun(t *testing.T) {
	m := sizeModel(t, testModel())
	m.input.Focus()
	updated, _ := m.testUpdate(keyRunes('q'))
	mm := updated.(*Model)
	if mm.quit || mm.state != stRunning || mm.input.Value() != "q" {
		t.Fatal("q must type a draft during inference")
	}
}
