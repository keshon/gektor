// Package cell owns terminal-cell geometry: fitting text to an exact width
// and padding a block to an exact height.
//
// Every overlay, form and popup needs the same two operations, and each one
// reimplemented them. They look trivial and are not: width is measured in
// terminal cells, not bytes or runes, so a CJK ideograph or an emoji counts
// as two, and truncation has to be ANSI-aware or it cuts a colour sequence
// in half and bleeds the rest of the frame.
package cell

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Line fits text into width cells: newlines flattened, truncated with an
// ellipsis, and right-padded so every line of a column lines up.
func Line(text string, width int) string {
	width = max(width, 0)
	text = ansi.Truncate(strings.ReplaceAll(text, "\n", " "), width, "…")
	return text + strings.Repeat(" ", max(width-lipgloss.Width(text), 0))
}

// Frame pads a block of already-fitted lines to exactly width x height.
// Short blocks get blank lines rather than being clipped, so a pane's
// background reaches the bottom border instead of ending mid-row.
func Frame(text string, width, height int) string {
	lines := strings.Split(text, "\n")
	out := make([]string, max(height, 0))
	for i := range out {
		if i < len(lines) {
			out[i] = Line(lines[i], width)
		} else {
			out[i] = strings.Repeat(" ", max(width, 0))
		}
	}
	return strings.Join(out, "\n")
}

// Width is the rendered cell width of the widest line in a block.
func Width(text string) int {
	w := 0
	for _, line := range strings.Split(text, "\n") {
		if n := lipgloss.Width(line); n > w {
			w = n
		}
	}
	return w
}

// Truncate shortens text to n cells with a marker. Unlike Line it does not
// pad, so it is for values rather than columns.
func Truncate(text string, n int) string {
	count := 0
	for i := range text {
		if count >= max(n, 0) {
			return text[:i] + "…"
		}
		count++
	}
	return text
}
