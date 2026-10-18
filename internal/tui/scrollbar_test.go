package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A scrollbar over a tall document must drag: press on the thumb,
// move, and the document follows the pointer. Clicks on the track
// page instead.
func scrollModel(t *testing.T, width, height int) *model {
	t.Helper()
	m := sizeModelTo(t, testModel(), width, height)
	for i := 0; i < 60; i++ {
		m.transcript.blocks = append(m.transcript.blocks, answerBlock(fmt.Sprintf("line %d", i)))
	}
	m.refreshContent()
	if !m.transcript.Viewport.Scrollable() {
		t.Fatal("fixture is not scrollable")
	}
	// Start at the top so every direction has somewhere to go.
	m.transcript.Viewport.GotoTop()
	m.transcript.Follow = false
	return m
}

func scrollbarPos(m *model) (x, top, paneH int) {
	return m.scrollbarX(), paneTop, m.transcript.Viewport.Height()
}

func TestScrollbarDragMovesTheDocument(t *testing.T) {
	m := scrollModel(t, 80, 24)
	x, _, h := scrollbarPos(m)
	thumbTop, _ := m.transcript.Viewport.ThumbRange(h)
	y := paneTop + thumbTop
	m.handleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !m.scrollDrag.active {
		t.Fatal("press on the thumb did not grab the scrollbar")
	}
	grab := m.scrollDrag.grab
	target := h - 1
	m.handleMouse(tea.MouseMotionMsg{X: x, Y: paneTop + target, Button: tea.MouseLeft})
	want := m.transcript.Viewport.DragOffset(target, h, grab)
	if got := m.transcript.Viewport.YOffset(); got != want {
		t.Fatalf("drag landed on %d, want %d", got, want)
	}
	if m.transcript.Follow != m.transcript.Viewport.AtBottom() {
		t.Fatal("follow stickiness diverged from the viewport after a drag")
	}
	m.handleMouse(tea.MouseReleaseMsg{X: x, Y: paneTop + target, Button: tea.MouseLeft})
	if m.scrollDrag.active {
		t.Fatal("release did not end the drag")
	}
}

func TestScrollbarTrackClickPages(t *testing.T) {
	m := scrollModel(t, 80, 24)
	x, _, h := scrollbarPos(m)
	thumbTop, size := m.transcript.Viewport.ThumbRange(h)
	if thumbTop+size >= h {
		t.Skip("thumb fills the track; no track to click")
	}
	y := paneTop + thumbTop + size
	if y >= paneTop+h {
		y = paneTop + h - 1
	}
	before := m.transcript.Viewport.YOffset()
	m.handleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if got := m.transcript.Viewport.YOffset(); got <= before {
		t.Fatalf("track click below the thumb did not page down (offset %d -> %d)", before, got)
	}
	if m.scrollDrag.active {
		t.Fatal("a click left a drag behind")
	}
}

func TestScrollbarClickOnThumbHoldsPosition(t *testing.T) {
	m := scrollModel(t, 80, 24)
	x, _, h := scrollbarPos(m)
	thumbTop, _ := m.transcript.Viewport.ThumbRange(h)
	y := paneTop + thumbTop
	before := m.transcript.Viewport.YOffset()
	m.handleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if got := m.transcript.Viewport.YOffset(); got != before {
		t.Fatalf("click on the thumb moved the document (%d -> %d)", before, got)
	}
}

func TestScrollbarIgnoresUnscrollableDocument(t *testing.T) {
	m := sizeModelTo(t, testModel(), 80, 24)
	m.transcript.blocks = []block{answerBlock("one")}
	m.refreshContent()
	if m.transcript.Viewport.Scrollable() {
		t.Fatal("one line should not scroll")
	}
	m.handleMouse(tea.MouseClickMsg{X: 10, Y: paneTop, Button: tea.MouseLeft})
	if m.scrollDrag.active {
		t.Fatal("an unscrollable document grabbed the scrollbar")
	}
}

func TestScrollbarDragEndsOffBar(t *testing.T) {
	m := scrollModel(t, 80, 24)
	x, _, h := scrollbarPos(m)
	thumbTop, _ := m.transcript.Viewport.ThumbRange(h)
	m.handleMouse(tea.MouseClickMsg{X: x, Y: paneTop + thumbTop, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseReleaseMsg{X: 1, Y: paneTop, Button: tea.MouseLeft})
	if m.scrollDrag.active {
		t.Fatal("release off the bar left the drag stuck")
	}
}
