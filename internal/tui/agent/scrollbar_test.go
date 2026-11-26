package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// A scrollbar over a tall document must drag: press on the thumb,
// move, and the document follows the pointer. Clicks on the track
// page instead.
func scrollModel(t *testing.T, width, height int) *Model {
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

func scrollbarPos(m *Model) (x, top, paneH int) {
	return m.scrollbarX(), paneTop, m.transcript.Viewport.Height()
}

func TestScrollbarDragMovesTheDocument(t *testing.T) {
	m := scrollModel(t, 80, 24)
	x, _, h := scrollbarPos(m)
	thumbTop, _ := m.transcript.Viewport.ThumbRange(h)
	y := paneTop + thumbTop
	m.handleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !m.scrollDrag.Active {
		t.Fatal("press on the thumb did not grab the scrollbar")
	}
	grab := m.scrollDrag.Grab
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
	if m.scrollDrag.Active {
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
	if m.scrollDrag.Active {
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
	if m.scrollDrag.Active {
		t.Fatal("an unscrollable document grabbed the scrollbar")
	}
}

func TestScrollbarDragEndsOffBar(t *testing.T) {
	m := scrollModel(t, 80, 24)
	x, _, h := scrollbarPos(m)
	thumbTop, _ := m.transcript.Viewport.ThumbRange(h)
	m.handleMouse(tea.MouseClickMsg{X: x, Y: paneTop + thumbTop, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseReleaseMsg{X: 1, Y: paneTop, Button: tea.MouseLeft})
	if m.scrollDrag.Active {
		t.Fatal("release off the bar left the drag stuck")
	}
}

func TestScrollbarResizeCancelsGrab(t *testing.T) {
	m := scrollModel(t, 100, 30)
	x, y, _ := scrollbarPos(m)
	m.testUpdate(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !m.scrollDrag.Active {
		t.Fatal("thumb did not capture mouse")
	}
	m.testUpdate(tea.WindowSizeMsg{Width: 80, Height: 22})
	m.testUpdate(tea.MouseMotionMsg{X: 0, Y: 100, Button: tea.MouseLeft})
	if m.scrollDrag.Active || m.transcript.Viewport.YOffset() != 0 {
		t.Fatal("resize kept the old thumb grab")
	}
}

func TestScrollbarGrabUsesDrawnColumnWithAndWithoutSidebar(t *testing.T) {
	for _, width := range []int{80, 140} {
		t.Run(fmt.Sprint("width=", width), func(t *testing.T) {
			m := scrollModel(t, width, 30)
			m.fitColumns()
			m.refreshContent()
			m.transcript.Viewport.GotoTop()
			// Select the actual rightmost content cell in the assembled frame,
			// independently of scrollbarHit/scrollbarX's coordinate arithmetic.
			row := []rune(strings.Split(ansi.Strip(m.testView().Content), "\n")[paneTop])
			x := len(row) - 2
			m.testUpdate(tea.MouseClickMsg{X: x, Y: paneTop, Button: tea.MouseLeft})
			if !m.scrollDrag.Active {
				t.Fatalf("drawn bar at x=%d did not grab", x)
			}
			m.testUpdate(tea.MouseMotionMsg{X: x - 3, Y: paneTop + 8, Button: tea.MouseLeft})
			if m.transcript.Viewport.YOffset() == 0 {
				t.Fatal("horizontal drift stopped drag")
			}
			// Move across the sidebar and outside the pane vertically. Capture
			// must keep ownership until release, without selecting a session.
			m.savedSessions = []sessionEntry{{id: "a"}, {id: "b"}}
			m.testUpdate(tea.MouseMotionMsg{X: 3, Y: m.termH - 1, Button: tea.MouseLeft})
			if !m.transcript.Viewport.AtBottom() {
				t.Fatal("drag was intercepted or failed to clamp below the pane")
			}
			m.testUpdate(tea.MouseMotionMsg{X: x + 3, Y: paneTop - 2, Button: tea.MouseLeft})
			if m.transcript.Viewport.YOffset() != 0 {
				t.Fatal("drag failed to clamp above the pane")
			}
			m.testUpdate(tea.MouseReleaseMsg{X: 3, Y: paneTop + 1, Button: tea.MouseLeft})
			if m.scrollDrag.Active {
				t.Fatal("sidebar intercepted release")
			}
		})
	}
}

func TestComposerCaretPhaseSurvivesScrollingAndBusyTicks(t *testing.T) {
	m := scrollModel(t, 80, 24)
	m.state = stRunning
	on := time.Unix(0, 0)
	m.testUpdate(caretTickMsg(on))
	if caret := m.testView().Cursor; caret == nil || caret.Blink {
		t.Fatal("expected a visible steady native caret")
	}

	m.testUpdate(caretTickMsg(on.Add(caretHalfPeriod)))
	m.testUpdate(tea.MouseWheelMsg{X: 10, Y: paneTop + 1, Button: tea.MouseWheelDown})
	m.testUpdate(tickMsg(time.Now()))
	if m.testView().Cursor != nil {
		t.Fatal("scroll or streaming tick restarted the caret phase")
	}
	m.testUpdate(caretTickMsg(on.Add(2 * caretHalfPeriod)))
	if m.testView().Cursor == nil {
		t.Fatal("caret did not return on the next phase")
	}
}
