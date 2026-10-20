package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/events"
)

// The wheel is the only way to read back while a run is streaming, and the
// only way to reach the rest of the transcript once a gate is open.
func TestWheelScrollsTheTranscriptInEveryStateThatShowsIt(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*model)
	}{
		{"idle", func(*model) {}},
		{"running", func(m *model) { m.state = stRunning }},
		{"permission gate", func(m *model) {
			m.handleEvent(events.Event{Name: "awaiting_input", Fields: map[string]any{
				"kind": "permission", "prompt": "ALLOW?"}})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := sizeModel(t, testModel())
			m.width(tea.WindowSizeMsg{Width: 100, Height: 30})
			for i := 0; i < 40; i++ {
				m.appendBlock(answerBlock("line"))
			}
			m.state = stDone
			m.refreshContent()
			c.setup(m)

			mid := m.transcript.Viewport.Total() / 2
			m.transcript.Viewport.SetYOffset(mid)
			_, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 60, Y: 10})
			if got := m.transcript.Viewport.YOffset(); got >= mid {
				t.Fatalf("wheel up did not scroll: %d -> %d", mid, got)
			}
			up := m.transcript.Viewport.YOffset()

			_, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 60, Y: 10})
			if got := m.transcript.Viewport.YOffset(); got <= up {
				t.Fatalf("wheel down did not scroll: %d -> %d", up, got)
			}
		})
	}
}

// A free-text question has no options, so no popup opens and the gate
// preview takes the pane. That is the one gate state where the transcript is
// not what is on screen, and the wheel must not pretend otherwise.
func TestWheelScrollsTheGatePreviewWhenItTakesThePane(t *testing.T) {
	m := sizeModel(t, testModel())
	m.width(tea.WindowSizeMsg{Width: 100, Height: 30})
	for i := 0; i < 40; i++ {
		m.appendBlock(answerBlock("line"))
	}
	m.state = stDone
	m.refreshContent()
	m.handleEvent(events.Event{Name: "awaiting_input", Fields: map[string]any{
		"kind": "question", "prompt": strings.Repeat("question line\n", 40)}})
	if m.gatePopup.Pending() {
		t.Fatal("a free-text question should not open an options popup")
	}
	before := m.transcript.Viewport.YOffset()
	_, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 60, Y: 10})
	if got := m.transcript.Viewport.YOffset(); got != before {
		t.Fatalf("the hidden transcript moved: %d -> %d", before, got)
	}
	if m.gateVP.YOffset() == 0 {
		t.Fatal("the gate preview on screen did not scroll")
	}
}

// A takeover overlay owns the pane while it is open. The wheel scrolls the
// dialog's own content, and must not reach the transcript behind it.
func TestWheelScrollsTheDialogNotTheTranscript(t *testing.T) {
	m := sizeModel(t, testModel())
	m.width(tea.WindowSizeMsg{Width: 100, Height: 30})
	for i := 0; i < 40; i++ {
		m.appendBlock(answerBlock("line"))
	}
	m.state = stDone
	m.refreshContent()
	m.transcript.Viewport.SetYOffset(m.transcript.Viewport.Total() / 2)
	before := m.transcript.Viewport.YOffset()

	rows := make([]string, 60)
	for i := range rows {
		rows[i] = "dialog line"
	}
	m.openDialog("long", rows)
	m.View() // the offset is clamped against the visible capacity at render
	if m.dialog.offset != 0 {
		t.Fatal("a dialog longer than the pane does not start at the top")
	}
	_, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 60, Y: 10})
	if m.dialog.offset != 1 {
		t.Fatalf("the dialog did not scroll: offset=%d", m.dialog.offset)
	}
	if got := m.transcript.Viewport.YOffset(); got != before {
		t.Fatalf("the transcript moved behind the overlay: %d -> %d", before, got)
	}
}

func TestWheelOverHistoryWorksBesideSessionList(t *testing.T) {
	m := scrollModel(t, 140, 30)
	m.nav.entries = []sessionEntry{{id: "a"}, {id: "b"}}
	m.navFocused = true
	m.input.Blur()
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: m.termW - 5, Y: paneTop + 3})
	if m.transcript.Viewport.YOffset() == 0 || m.nav.Cursor != 0 {
		t.Fatal("wheel over history failed or changed session selection")
	}
}
