package gate

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/tui/components/popup"
)

func testStyles() popup.Styles {
	return popup.Styles{
		Normal:   lipgloss.NewStyle(),
		Selected: lipgloss.NewStyle(),
	}
}

func threeOptions() []Option {
	return []Option{
		{Value: "y", Label: "Allow once", Hint: "Y"},
		{Value: "a", Label: "Allow this run", Hint: "A"},
		{Value: "n", Label: "Deny with note", Hint: "N"},
	}
}

// The caller splices these rows over a fixed-height body and drops the whole
// popup when it does not fit. Overshooting the height by one row is
// therefore not a cosmetic bug: the gate silently disappears, leaving a run
// blocked behind a question nobody can see or answer.
func TestViewNeverExceedsItsHeight(t *testing.T) {
	long := make([]string, 200)
	for i := range long {
		long[i] = "prompt line"
	}
	for _, height := range []int{4, 5, 6, 7, 8, 10, 14, 20, 40} {
		for _, note := range []bool{false, true} {
			for _, q := range [][]string{nil, {"one line"}, long} {
				s := &State{}
				s.Open("Permission required", threeOptions())
				rows := s.View(60, height, q, "Enter confirm", note, testStyles())
				if len(rows) > height {
					t.Fatalf("height %d, note=%v, question %d lines: drew %d rows",
						height, note, len(q), len(rows))
				}
			}
		}
	}
}

// With room, the answers are the point of the popup and must not be what
// gets truncated away.
func TestOptionsSurviveWhenThereIsRoom(t *testing.T) {
	s := &State{}
	s.Open("Permission required", threeOptions())
	rows := s.View(60, 12, []string{"a", "b", "c", "d", "e", "f", "g", "h"}, "hint", false, testStyles())
	joined := strip(rows)
	for _, want := range []string{"Allow once", "Allow this run", "Deny with note"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("lost %q:\n%s", want, joined)
		}
	}
}

// A long prompt is truncated with a count, because the full text is in the
// transcript behind the popup and silence would read as "that is all of it".
func TestLongPromptSaysHowMuchIsBehind(t *testing.T) {
	question := make([]string, 60)
	for i := range question {
		question[i] = "line"
	}
	s := &State{}
	s.Open("Permission required", threeOptions())
	rows := s.View(60, 12, question, "hint", false, testStyles())
	joined := strip(rows)
	if !strings.Contains(joined, "more lines") && !strings.Contains(joined, "transcript") {
		t.Fatalf("truncated without saying what was hidden:\n%s", joined)
	}
}

// Too small for a question, the answers still win - but a row that was
// going to be spent saying so has to actually exist before it is drawn.
func TestTinyHeightKeepsTheAnswersAndFits(t *testing.T) {
	s := &State{}
	s.Open("Permission required", threeOptions())
	rows := s.View(60, 6, []string{"a", "b", "c", "d", "e"}, "hint", false, testStyles())
	if len(rows) > 6 {
		t.Fatalf("drew %d rows into 6", len(rows))
	}
	joined := strip(rows)
	for _, want := range []string{"Allow once", "Allow this run", "Deny with note"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("lost %q at height 6:\n%s", want, joined)
		}
	}
}

// With a spare row, a question that did not fit says so rather than reading
// as the whole of it.
func TestSpareRowIsSpentExplainingTheHiddenQuestion(t *testing.T) {
	s := &State{}
	s.Open("Permission required", threeOptions())
	rows := s.View(60, 9, []string{"a", "b", "c", "d", "e", "f"}, "hint", false, testStyles())
	if len(rows) > 9 {
		t.Fatalf("drew %d rows into 9", len(rows))
	}
	if !strings.Contains(strip(rows), "transcript") {
		t.Fatalf("a hidden question was not reported:\n%s", strip(rows))
	}
}

func TestSelectionWrapsAtBothEnds(t *testing.T) {
	s := &State{}
	s.Open("t", threeOptions())
	if s.Current().Value != "y" {
		t.Fatalf("starts on %q", s.Current().Value)
	}
	s.Move(-1)
	if s.Current().Value != "n" {
		t.Fatalf("up from the first row went to %q", s.Current().Value)
	}
	s.Move(1)
	if s.Current().Value != "y" {
		t.Fatalf("down from the last row went to %q", s.Current().Value)
	}
	s.Selected = 2
	if got := s.Current().Value; got != "n" {
		t.Fatalf("Current = %q", got)
	}
}

// The selection has to be findable in the drawn rows, which is the only
// thing that proves a long list scrolled to it.
func TestLongListScrollsToTheSelection(t *testing.T) {
	options := make([]Option, 30)
	for i := range options {
		options[i] = Option{Value: "v", Label: "option"}
	}
	s := &State{}
	s.Open("many", options)
	s.Selected = 25
	rows := s.View(60, 12, []string{"q"}, "hint", false, testStyles())
	markers := 0
	for _, r := range rows {
		if strings.Contains(ansi.Strip(r), "›") {
			markers++
		}
	}
	if markers != 1 {
		t.Fatalf("%d selected rows drawn; the list did not scroll to the selection:\n%s",
			markers, strip(rows))
	}
}

func TestCloseEmptiesThePopup(t *testing.T) {
	s := &State{}
	s.Open("t", threeOptions())
	if !s.Pending() {
		t.Fatal("Open did not make the popup pending")
	}
	s.Close()
	if s.Pending() || s.Selected != 0 {
		t.Fatalf("Close left pending=%v selected=%d", s.Pending(), s.Selected)
	}
	if s.Keypress(tea.KeyPressMsg{Code: tea.KeyDown}) {
		t.Fatal("a closed popup consumed a key")
	}
	s.Move(1) // must not panic on an empty list
	if s.Selected != 0 {
		t.Fatalf("Move on a closed popup changed the selection to %d", s.Selected)
	}
}

func strip(rows []string) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ansi.Strip(r)
	}
	return strings.Join(out, "\n")
}
