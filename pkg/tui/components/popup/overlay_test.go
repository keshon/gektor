package popup

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The overlay splice was written twice in the root package, differing only in
// variable names. The columns between a short row and its declared frame are
// background: cutting from the row's own end instead of the frame's width
// ate them, which is how the search popup came to overwrite the whole line.
func TestOverlayKeepsBackgroundBesideAShortRow(t *testing.T) {
	background := strings.Repeat(strings.Repeat("x", 20)+"\n", 3)
	plain := ansi.Strip(strings.Split(Overlay(background, []string{"AB"}, 4, 1, 10, 20), "\n")[1])
	// Columns 0-3 and 14-19 are background; 4-13 are the frame's width, of
	// which the row fills two.
	if want := "xxxx" + "AB" + "xxxxxx"; plain != want {
		t.Fatalf("got %q, want %q", plain, want)
	}
}

// Rows past the end of the screen are dropped, not appended: the background is
// already the terminal's height, so an overflow would scroll the frame out.
func TestOverlayDropsRowsPastTheScreen(t *testing.T) {
	plain := ansi.Strip(Overlay("one\ntwo", []string{"A", "B", "C"}, 0, 0, 3, 3))
	if strings.Count(plain, "\n") != 1 {
		t.Fatalf("grew the screen: %q", plain)
	}
	if plain != "A\nB" {
		t.Fatalf("got %q, want %q", plain, "A\nB")
	}
}
