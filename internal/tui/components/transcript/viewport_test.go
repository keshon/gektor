package transcript

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	keyPgUp   = tea.KeyPressMsg{Code: tea.KeyPgUp}
	keyPgDown = tea.KeyPressMsg{Code: tea.KeyPgDown}
)

// The window draws only the rows it is showing, in the full width, with a
// scrollbar in the last column when the document is taller than it.
func TestWindowIsExactlyItsSize(t *testing.T) {
	m := New()
	m.Viewport = NewViewport(20, 6)
	parts := make([]string, 40)
	for i := range parts {
		parts[i] = "block"
	}
	m.SetParts(parts, "")
	for _, offset := range []int{0, 1, 7, m.Viewport.Total() - 6} {
		m.Viewport.SetYOffset(offset)
		rows := strings.Split(m.View(), "\n")
		if len(rows) != 6 {
			t.Fatalf("offset %d: %d rows, want 6", offset, len(rows))
		}
		for i, r := range rows {
			if w := lipgloss.Width(ansi.Strip(r)); w != 20 {
				t.Fatalf("offset %d row %d is %d cells, want 20", offset, i, w)
			}
		}
	}
}

// A scrollbar that shows nothing about where you are is decoration. The
// thumb must sit where the reader is, at both ends and in between.
func TestScrollbarThumbFollowsTheReader(t *testing.T) {
	m := New()
	m.Viewport = NewViewport(10, 10)
	parts := make([]string, 100)
	for i := range parts {
		parts[i] = "block"
	}
	m.SetParts(parts, "")

	thumbAt := func() int {
		for i, row := range strings.Split(m.View(), "\n") {
			if strings.Contains(ansi.Strip(row), "█") {
				return i
			}
		}
		return -1
	}
	m.Viewport.GotoTop()
	if got := thumbAt(); got != 0 {
		t.Fatalf("at the top the thumb is on row %d, want 0", got)
	}
	m.Viewport.GotoBottom()
	if got := thumbAt(); got != 9 {
		t.Fatalf("at the bottom the thumb is on row %d, want 9", got)
	}
	m.Viewport.SetYOffset(m.Viewport.Total() / 2)
	mid := thumbAt()
	if mid <= 0 || mid >= 9 {
		t.Fatalf("halfway down the thumb is on row %d, which is not halfway", mid)
	}
}

// The thumb has to be able to show the visible fraction, otherwise a reader
// who has found something cannot tell how much is left.
func TestScrollbarThumbTracksTheVisibleFraction(t *testing.T) {
	thumbRows := func(total int) int {
		m := New()
		m.Viewport = NewViewport(10, 10)
		parts := make([]string, total)
		for i := range parts {
			parts[i] = "block"
		}
		m.SetParts(parts, "")
		m.Viewport.GotoTop()
		n := 0
		for _, row := range strings.Split(m.View(), "\n") {
			if strings.Contains(ansi.Strip(row), "█") {
				n++
			}
		}
		return n
	}
	small, large := thumbRows(11), thumbRows(200)
	if small <= large {
		t.Fatalf("thumb is %d rows for 11 lines and %d for 200; it does not shrink",
			small, large)
	}
	if large < 1 || small > 10 {
		t.Fatalf("thumb sizes out of range: %d and %d", small, large)
	}
}

// A document that already fits has nothing to scroll, so it gets no bar and
// keeps the column it would otherwise lose.
func TestNoScrollbarWhenEverythingFits(t *testing.T) {
	m := New()
	m.Viewport = NewViewport(20, 10)
	m.SetParts([]string{"one", "two"}, "")
	if m.Viewport.Scrollable() {
		t.Fatal("a three-line document in a ten-row window is not scrollable")
	}
	if got := m.Viewport.ContentWidth(); got != 20 {
		t.Fatalf("ContentWidth = %d, want the full 20", got)
	}
	for _, row := range strings.Split(m.View(), "\n") {
		if strings.ContainsAny(ansi.Strip(row), "╎█") {
			t.Fatalf("a fitting document drew a scrollbar:\n%s", m.View())
		}
	}
}

// Text is rendered at ContentWidth, so the bar must not eat a column of it.
func TestScrollbarTakesItsColumnFromTheText(t *testing.T) {
	m := New()
	m.Viewport = NewViewport(20, 3)
	parts := make([]string, 10)
	for i := range parts {
		parts[i] = strings.Repeat("x", 40)
	}
	m.SetParts(parts, "")
	if got := m.Viewport.ContentWidth(); got != 18 {
		t.Fatalf("ContentWidth = %d, want 18 plus gutter and scrollbar", got)
	}
	for i, row := range strings.Split(m.View(), "\n") {
		plain := ansi.Strip(row)
		if got := lipgloss.Width(plain); got != 20 {
			t.Fatalf("row %d is %d cells, want 20", i, got)
		}
		if []rune(plain)[18] != ' ' {
			t.Fatalf("row %d has no gutter before the scrollbar: %q", i, plain)
		}
		if strings.Contains(plain, "x╎") || strings.Contains(plain, "x█") {
			t.Fatalf("row %d ran text under the scrollbar: %q", i, plain)
		}
	}
}

// Paging is the only keyboard scrolling the application routes here. Both
// directions must move, and neither may run off either end.
func TestPagingMovesBothWaysAndStopsAtTheEnds(t *testing.T) {
	m := New()
	m.Viewport = NewViewport(20, 6)
	parts := make([]string, 40)
	for i := range parts {
		parts[i] = "block"
	}
	m.SetParts(parts, "")

	m.Viewport.GotoBottom()
	m.Update(keyPgUp)
	if m.Viewport.YOffset() <= 0 {
		t.Fatal("page up at the bottom did not move")
	}
	before := m.Viewport.YOffset()
	m.Update(keyPgDown)
	if m.Viewport.YOffset() <= before {
		t.Fatal("page down did not move")
	}
	m.Viewport.GotoTop()
	m.Update(keyPgUp)
	if m.Viewport.YOffset() != 0 {
		t.Fatalf("page up past the top landed on %d", m.Viewport.YOffset())
	}
	m.Viewport.GotoBottom()
	before = m.Viewport.YOffset()
	m.Update(keyPgDown)
	if m.Viewport.YOffset() != before {
		t.Fatalf("page down past the bottom landed on %d", m.Viewport.YOffset())
	}
}
