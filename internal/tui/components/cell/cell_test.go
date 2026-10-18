package cell

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// Width is measured in terminal cells, not runes: a CJK ideograph and an
// emoji occupy two columns each, so a rune-counting padder produces ragged
// columns and a rune-counting truncator splits the box.
func TestLineFitsByCellWidth(t *testing.T) {
	for _, s := range []string{"abc", "日本語", "a日b", "👍 ok"} {
		got := Line(s, 10)
		if w := lipgloss.Width(got); w != 10 {
			t.Fatalf("Line(%q, 10) is %d cells, want 10", s, w)
		}
	}
}

func TestLineTruncatesWithAMarker(t *testing.T) {
	got := Line("abcdefghij", 5)
	if w := lipgloss.Width(got); w != 5 {
		t.Fatalf("width = %d, want 5", w)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncation left no marker: %q", got)
	}
}

// Truncating a styled string must not split an escape sequence, or the
// frame inherits a colour it was never given.
func TestLineTruncationKeepsStylesIntact(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render("abcdefghij")
	got := Line(styled, 5)
	if strings.Count(got, "\x1b")%2 != 0 {
		t.Fatalf("truncation split an escape sequence: %q", got)
	}
	if w := lipgloss.Width(got); w != 5 {
		t.Fatalf("width = %d, want 5", w)
	}
}

func TestLineFlattensNewlines(t *testing.T) {
	got := Line("one\ntwo", 8)
	if strings.Contains(got, "\n") {
		t.Fatal("Line kept a newline; a column row must be one line")
	}
	if w := lipgloss.Width(got); w != 8 {
		t.Fatalf("width = %d, want 8", w)
	}
}

func TestLineHandlesNonPositiveWidth(t *testing.T) {
	if got := Line("abc", 0); got != "" {
		t.Fatalf("Line(s, 0) = %q, want empty", got)
	}
	if got := Line("abc", -5); got != "" {
		t.Fatalf("Line(s, -5) = %q, want empty", got)
	}
}

func TestFramePadsToExactHeight(t *testing.T) {
	got := Frame("a\nb", 4, 4)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(lines))
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 4 {
			t.Fatalf("line %d is %d cells, want 4", i, w)
		}
	}
	if lines[3] != "    " {
		t.Fatalf("padding row = %q, want blanks", lines[3])
	}
}

func TestFrameClipsExtraLines(t *testing.T) {
	got := Frame("a\nb\nc", 4, 2)
	if n := len(strings.Split(got, "\n")); n != 2 {
		t.Fatalf("got %d lines, want 2", n)
	}
}

func TestWidthFindsTheWidestLine(t *testing.T) {
	if got := Width("a\nbbbb\ncc"); got != 4 {
		t.Fatalf("Width = %d, want 4", got)
	}
	if got := Width("日本\nab"); got != 4 {
		t.Fatalf("Width = %d, want 4 for CJK", got)
	}
}

func TestTruncateDoesNotPad(t *testing.T) {
	if got := Truncate("abc", 10); got != "abc" {
		t.Fatalf("Truncate padded: %q", got)
	}
	if got := Truncate("abcdef", 3); !strings.HasSuffix(got, "…") {
		t.Fatalf("Truncate = %q, want a marker", got)
	}
}