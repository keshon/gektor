package agent

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
