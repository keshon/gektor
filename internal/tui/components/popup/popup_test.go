package popup

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func testStyles() Styles {
	return Styles{
		Normal:   lipgloss.NewStyle().Background(lipgloss.Color("#263744")),
		Border:   lipgloss.NewStyle().Background(lipgloss.Color("#263744")),
		Selected: lipgloss.NewStyle().Background(lipgloss.Color("#3B526F")),
	}
}

func TestFrameIsRectangular(t *testing.T) {
	const width = 10
	inner := width - 4
	rows := []string{
		Row(testStyles().Normal, "a", inner),
		Row(testStyles().Normal, "bb", inner),
	}
	framed := TitledFrame("", rows, width, testStyles())
	if len(framed) != 4 {
		t.Fatalf("got %d lines, want 4 (2 rows + 2 borders)", len(framed))
	}
	for i, l := range framed {
		if got := lipgloss.Width(ansi.Strip(l)); got != width {
			t.Fatalf("line %d is %d cells, want %d", i, got, width)
		}
	}
	if !strings.HasPrefix(ansi.Strip(framed[0]), "╭") || !strings.HasSuffix(ansi.Strip(framed[0]), "╮") {
		t.Fatalf("top edge is not closed: %q", ansi.Strip(framed[0]))
	}
	if b := ansi.Strip(framed[len(framed)-1]); !strings.HasPrefix(b, "╰") || !strings.HasSuffix(b, "╯") {
		t.Fatalf("bottom edge is not closed: %q", b)
	}
}

func TestTitledFrameFitsItsTitle(t *testing.T) {
	const width = 40
	rows := []string{Row(testStyles().Normal, "x", width-4)}
	for _, title := range []string{"", "Files", "Commands"} {
		framed := TitledFrame(title, rows, width, testStyles())
		for i, l := range framed {
			if got := lipgloss.Width(ansi.Strip(l)); got != width {
				t.Fatalf("title %q: line %d is %d cells, want %d", title, i, got, width)
			}
		}
		if title != "" && !strings.Contains(ansi.Strip(framed[0]), title) {
			t.Fatalf("title %q missing from the top border", title)
		}
	}
}

// A frame that has lost its own title is worse than one cell too wide.
func TestTitledFrameWidensRatherThanClippingItsTitle(t *testing.T) {
	rows := []string{Row(testStyles().Normal, "x", 36)}
	framed := TitledFrame("a rather long popup title", rows, 8, testStyles())
	if !strings.Contains(ansi.Strip(framed[0]), "a rather long popup title") {
		t.Fatalf("title clipped at a narrow width: %q", ansi.Strip(framed[0]))
	}
}

// Every overlay draws its rows the same way; a drift here is what makes two
// popups look like different products.
func TestRowKeepsTheSurfaceThroughNestedStyles(t *testing.T) {
	// Feed the renderer every reset a nested style can emit, so the test
	// checks the contract rather than one lipgloss version's spelling.
	got := Row(testStyles().Normal, "\x1b[m\x1b[0m\x1b[49mx", 20)
	for _, reset := range []string{"\x1b[m", "\x1b[0m", "\x1b[49m"} {
		if !strings.Contains(got, reset+"\x1b[48;2;") {
			t.Fatalf("%q did not restore the surface after it:\n%q", reset, got)
		}
	}
	if !strings.HasSuffix(got, "\x1b[m") {
		t.Fatal("row did not close with a reset; the colour would bleed onto the frame")
	}
}

func TestRowFitsExactWidth(t *testing.T) {
	styled := lipgloss.NewStyle().Bold(true).Render("some text here")
	got := Row(testStyles().Normal, styled, 12)
	if w := lipgloss.Width(ansi.Strip(got)); w != 12 {
		t.Fatalf("row is %d cells, want 12", w)
	}
}

func TestBoxSizesItselfToItsWidestRow(t *testing.T) {
	framed := Box([]string{"a", "bbbb", "cc"}, testStyles())
	if len(framed) != 5 {
		t.Fatalf("got %d lines, want 5", len(framed))
	}
	width := lipgloss.Width(ansi.Strip(framed[0]))
	for i, l := range framed {
		if got := lipgloss.Width(ansi.Strip(l)); got != width {
			t.Fatalf("line %d is %d cells, want %d", i, got, width)
		}
	}
}

func TestFrameBoxCentres(t *testing.T) {
	box := FrameBox([]string{"one", "two"}, 30, 10, testStyles())
	lines := strings.Split(box, "\n")
	if strings.TrimSpace(lines[0]) != "" {
		t.Fatal("box is flush to the top; it should be centred")
	}
	top := 0
	for top < len(lines) && strings.TrimSpace(lines[top]) == "" {
		top++
	}
	if !strings.HasPrefix(strings.TrimSpace(ansi.Strip(lines[top])), "╭") {
		t.Fatalf("expected the frame to start at line %d", top)
	}
	if !strings.HasPrefix(lines[top], " ") {
		t.Fatal("box is flush to the left; it should be centred")
	}
	for i, l := range lines[top:] {
		if strings.TrimSpace(l) == "" {
			t.Fatalf("line %d inside the box is blank", i+top)
		}
	}
}