package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/events"
)

// A thinking model's turn must read as THINK then RESPONSE: deliberation
// filling live, and still on screen when the answer starts.
//
// Reasoning used to be accumulated silently in the llm layer and attached
// to the finished message, so it appeared in one lump after the whole
// reply had streamed. One merged buffer would not have fixed that either —
// it would have interleaved the two.
func TestThinkStreamsBeforeResponse(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning

	for _, chunk := range []string{"checking ", "the ", "parser"} {
		updated, _ := m.Update(eventMsg(events.Event{Name: "reasoning", Fields: map[string]any{"text": chunk}}))
		m = updated.(*model)
	}
	m = advanceFrames(t, m)

	if m.liveThink == "" {
		t.Fatal("nothing was revealed while the model was still thinking")
	}
	if m.live != "" {
		t.Fatalf("answer text appeared before any was streamed: %q", m.live)
	}
	if !strings.Contains(m.liveThink, "parser") {
		t.Fatalf("think block = %q", m.liveThink)
	}

	// The answer now starts, with the reasoning still held above it.
	for _, chunk := range []string{"Here ", "is ", "the fix"} {
		updated, _ := m.Update(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": chunk}}))
		m = updated.(*model)
	}
	m = advanceFrames(t, m)

	if !strings.Contains(m.liveThink, "parser") {
		t.Fatalf("reasoning was dropped when the answer began: %q", m.liveThink)
	}
	if !strings.Contains(m.live, "the fix") {
		t.Fatalf("answer = %q", m.live)
	}

	// Rendered, THINK sits above RESPONSE in one live turn.
	view := ansi.Strip(m.View().Content)
	thinkAt := strings.Index(view, "parser")
	answerAt := strings.Index(view, "the fix")
	if thinkAt < 0 || answerAt < 0 {
		t.Fatalf("missing from the rendered turn:\n%s", view)
	}
	if thinkAt > answerAt {
		t.Fatalf("THINK must render above RESPONSE:\n%s", view)
	}
}

// Reasoning and answer text must never share a buffer.
func TestThinkAndResponseStaySeparate(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	m.Update(eventMsg(events.Event{Name: "reasoning", Fields: map[string]any{"text": "pondering"}}))
	m.Update(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": "answering"}}))
	m = advanceFrames(t, m)

	if strings.Contains(m.live, "pondering") {
		t.Fatalf("reasoning leaked into the answer buffer: %q", m.live)
	}
	if strings.Contains(m.liveThink, "answering") {
		t.Fatalf("answer leaked into the think buffer: %q", m.liveThink)
	}
}

// A new turn must not inherit the previous turn's reasoning.
func TestResetLiveClearsThink(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	updated, _ := m.Update(eventMsg(events.Event{Name: "reasoning", Fields: map[string]any{"text": "old thought"}}))
	m = updated.(*model)
	updated, _ = m.Update(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": "old answer"}}))
	m = updated.(*model)
	m = advanceFrames(t, m)

	m.resetLive()
	if m.liveThink != "" || m.liveThinkQueue != "" || m.live != "" || m.liveQueue != "" {
		t.Fatalf("reset left state behind: think=%q/%q answer=%q/%q",
			m.liveThink, m.liveThinkQueue, m.live, m.liveQueue)
	}
}