package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/tui/components/popup"
)

// Column alignment is measured in cells. This one was measured in bytes, so a
// label with anything non-ASCII pushed its own value out of line - and the
// only other aligners in the package already did it correctly, which is what
// makes a third spelling worth removing rather than leaving.
func TestKvRowsAlignsNonASCIIColumns(t *testing.T) {
	rows := kvRows([]string{
		"Backend: llama",
		"Модель: local",
		"Mode: act",
	})
	want := -1
	for i, r := range rows {
		plain := ansi.Strip(r)
		j := strings.Index(plain, ":")
		if j < 0 {
			t.Fatalf("row %d lost its colon: %q", i, plain)
		}
		if w := lipgloss.Width(plain[:j]); w >= 0 {
			if want < 0 {
				want = w
			} else if w != want {
				t.Fatalf("row %d label is %d cells, row 0 is %d:\n%s",
					i, w, want, strings.Join(rows, "\n"))
			}
		}
	}
	// And the values, not the colons, are what line up.
	for i, r := range rows {
		plain := ansi.Strip(r)
		if at := lipgloss.Width(plain[:strings.Index(plain, ":")+1]); at != want+1 {
			t.Fatalf("row %d value starts at column %d, want %d", i, at, want+1)
		}
	}
}

// The overlay splice was written twice in the root package, differing only in
// variable names. The columns between a short row and its declared frame are
// background: cutting from the row's own end instead of the frame's width
// ate them, which is how the search popup came to overwrite the whole line.
func TestOverlayKeepsBackgroundBesideAShortRow(t *testing.T) {
	background := strings.Repeat(strings.Repeat("x", 20)+"\n", 3)
	plain := ansi.Strip(strings.Split(popup.Overlay(background, []string{"AB"}, 4, 1, 10, 20), "\n")[1])
	// Columns 0-3 and 14-19 are background; 4-13 are the frame's width, of
	// which the row fills two.
	if want := "xxxx" + "AB" + "xxxxxx"; plain != want {
		t.Fatalf("got %q, want %q", plain, want)
	}
}

// Rows past the end of the screen are dropped, not appended: the background is
// already the terminal's height, so an overflow would scroll the frame out.
func TestOverlayDropsRowsPastTheScreen(t *testing.T) {
	plain := ansi.Strip(popup.Overlay("one\ntwo", []string{"A", "B", "C"}, 0, 0, 3, 3))
	if strings.Count(plain, "\n") != 1 {
		t.Fatalf("grew the screen: %q", plain)
	}
	if plain != "A\nB" {
		t.Fatalf("got %q, want %q", plain, "A\nB")
	}
}
