package agent

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/events"
)

func TestThinkCollapsedByDefault(t *testing.T) {
	st := defaultStyles()
	long := "...The user is asking something, and here is a long chain of internal deliberation that must not read as the answer. " +
		"Second line of musing that only the expanded view may show."
	collapsed := renderBlock(thinkBlock(long), st, tagged(), 80)
	if !strings.Contains(collapsed, "[THINKING]") {
		t.Fatalf("collapsed thinking needs its header: %q", collapsed)
	}
	if !strings.Contains(collapsed, "chars]") {
		t.Fatalf("collapsed thinking shows size only here: %q", collapsed)
	}
	if strings.Contains(collapsed, "Second line of musing") {
		t.Fatalf("collapsed thinking leaks body: %q", collapsed)
	}
	expanded := renderBlock(thinkBlock(long), st, renderOpts{expand: true, labels: true}, 80)
	// Wrapping splits phrases across rail-indented lines: normalize
	// all whitespace before matching.
	flat := strings.Join(strings.Fields(ansi.Strip(expanded)), " ")
	if !strings.Contains(flat, "Second line of musing") {
		t.Fatalf("expanded thinking must show all: %q", expanded)
	}
	if strings.Contains(expanded, "chars]") {
		t.Fatalf("expanded thinking must not repeat the size: %q", expanded)
	}
}

func TestThinkToggleKey(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	if !m.transcript.compact {
		t.Fatal("history starts compact")
	}
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if mm := updated.(*Model); mm.transcript.compact {
		t.Fatal("ctrl+g must open full view")
	} else {
		updated, _ = mm.testUpdate(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
		if mm := updated.(*Model); !mm.transcript.compact {
			t.Fatal("ctrl+g again must compact")
		}
	}
	// A plain "t" types, never toggles.
	mm := sizeModel(t, testModel())
	mm.state = stDone
	mm.input.SetValue("ed")
	updated, _ = mm.testUpdate(tea.KeyPressMsg{Text: string([]rune{'t'})})
	if mm := updated.(*Model); !mm.transcript.compact || mm.input.Value() != "edt" {
		t.Fatalf("t must type, not toggle: compact=%v value=%q", mm.transcript.compact, mm.input.Value())
	}
}

func TestHelpMentionsThinkKey(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("/help")
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: tea.KeyTab})
	updated, _ = updated.(*Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := updated.(*Model)
	mm.chrome.Dialog.Page = 1
	if !strings.Contains(mm.testControls().DialogView(), "Ctrl+G") {
		t.Fatal("help must document the think key")
	}
}

func TestToolContentVisibilityIsIndependentOfThinking(t *testing.T) {
	st := defaultStyles()
	text := strings.TrimRight(strings.Repeat("abcdef\n", 25), "\n")
	mk := func() block {
		return block{role: roleTool, text: "run x", callID: "c1", result: text}
	}
	hidden := ansi.Strip(renderBlock(mk(), st, renderOpts{hideTools: true, labels: true}, 80))
	if strings.Contains(hidden, "abcdef") || !strings.Contains(hidden, "run x") || !strings.Contains(hidden, "✓") {
		t.Fatal("tool metadata leaked content", hidden)
	}
	full := renderBlock(mk(), st, tagged(), 80)
	if n := strings.Count(full, "abcdef"); n != 25 {
		t.Fatalf("full view shows %d lines, want 25", n)
	}
	if strings.Contains(full, "more lines") {
		t.Fatalf("full view must not mark: %q", full)
	}
}

func TestLiveAccumulatesAndStepReplaces(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	updated, _ := m.testUpdate(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": "hel"}}))
	mm := updated.(*Model)
	if mm.live != "" || mm.liveQueue != "hel" {
		t.Fatalf("a delta must wait only for the next frame: live=%q queue=%q", mm.live, mm.liveQueue)
	}
	updated, _ = mm.testUpdate(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": "lo"}}))
	mm = advanceFrames(t, updated)
	if mm.live != "hello" || mm.liveQueue != "" {
		t.Fatalf("live = %q queue = %q", mm.live, mm.liveQueue)
	}
	// Step replaces without duplication.
	updated, _ = mm.testUpdate(eventMsg(events.Event{Name: "step", Fields: map[string]any{"text": "hello"}}))
	mm = updated.(*Model)
	if mm.live != "" || mm.liveQueue != "" {
		t.Fatal("step must clear the live buffer")
	}
	if n := strings.Count(mm.transcript.Viewport.View(), "hello"); n != 1 {
		t.Fatalf("hello appears %d times, want exactly 1", n)
	}
}

// A step that omits the reply adopts everything that arrived, including
// text still queued for reveal. Otherwise the tail of a streamed reply
// would be lost when the backend sends the step before the reader has
// seen it.
func TestLiveQueuedTextIsAdoptedByStep(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	updated, _ := m.testUpdate(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": "unseen tail"}}))
	mm := updated.(*Model)
	if mm.liveQueue == "" {
		t.Fatal("precondition: text should be awaiting its frame")
	}
	updated, _ = mm.testUpdate(eventMsg(events.Event{Name: "step", Fields: map[string]any{}}))
	mm = updated.(*Model)
	if mm.live != "" || mm.liveQueue != "" {
		t.Fatal("step must clear both buffers")
	}
	found := false
	for _, b := range mm.transcript.blocks {
		if b.role == roleAnswer && strings.Contains(b.text, "unseen tail") {
			found = true
		}
	}
	if !found {
		t.Fatalf("queued text was dropped by the step: %+v", mm.transcript.blocks)
	}
}

func TestLiveAdoptedWhenStepOmits(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": "visible"}}))
	mm := updated.(*Model)
	updated, _ = mm.testUpdate(eventMsg(events.Event{Name: "step", Fields: map[string]any{}}))
	mm = updated.(*Model)
	found := false
	for _, b := range mm.transcript.blocks {
		if b.role == roleAnswer && strings.Contains(b.text, "visible") {
			found = true
		}
	}
	if !found {
		t.Fatalf("omitted step must adopt the buffer: %+v", mm.transcript.blocks)
	}
}

func TestLiveDiscardedOnDone(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": "partial"}}))
	mm := updated.(*Model)
	updated, _ = mm.testUpdate(doneMsg{answer: "done"})
	if mm := updated.(*Model); mm.live != "" {
		t.Fatal("done must discard partial live text")
	}
}

func TestLiveUnfollowedStability(t *testing.T) {
	m := sizeModel(t, testModel())
	for i := 0; i < 40; i++ {
		m.appendBlock(markerBlock("filler"))
	}
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: tea.KeyPgUp})
	mm := updated.(*Model)
	if mm.transcript.Follow {
		t.Fatal("setup: pgup must unfollow")
	}
	y0 := mm.transcript.Viewport.YOffset()
	for _, chunk := range []string{"a", "b", "c", "d", "e"} {
		updated, _ = mm.testUpdate(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": chunk}}))
		mm = updated.(*Model)
	}
	mm = advanceFrames(t, mm)
	if mm.transcript.Viewport.YOffset() != y0 {
		t.Fatalf("live growth below moved offset %d -> %d", y0, mm.transcript.Viewport.YOffset())
	}
	if mm.live != "abcde" {
		t.Fatalf("live = %q", mm.live)
	}
}

// A reader who scrolled away is not watching the tail, so the reveal
// empties the queue in one frame instead of animating text nobody can
// see. The frames are the scarce resource.
func TestUnfollowedRevealSkipsTheAnimation(t *testing.T) {
	m := sizeModel(t, testModel())
	for i := 0; i < 40; i++ {
		m.appendBlock(markerBlock("filler"))
	}
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyPgUp})
	m.state = stRunning
	updated, _ := m.testUpdate(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": strings.Repeat("z", 400)}}))
	mm := updated.(*Model)
	if mm.transcript.Follow {
		t.Fatal("setup: must stay unfollowed")
	}
	if mm.revealLive() || mm.liveQueue != "" {
		t.Fatalf("one frame must empty the queue while unfollowed: %q left", mm.liveQueue)
	}
	if n := len([]rune(mm.live)); n != 400 {
		t.Fatalf("revealed %d runes, want 400", n)
	}
}

// Coalescing must drain the complete backlog in one frame, regardless of size.
func TestRevealHasNoArtificialTypingDelay(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning

	// A slow backend: one character queued reveals one character.
	m.liveQueue = "a"
	m.revealLive()
	if m.live != "a" {
		t.Fatalf("slow stream revealed %q, want one character", m.live)
	}

	// A fast backend: a 4000-character backlog must not take 4000 frames.
	m.live, m.liveQueue = "", strings.Repeat("x", 4000)
	if m.revealLive() || m.liveQueue != "" {
		t.Fatal("a received burst must paint in one update")
	}
	if n := len([]rune(m.live)); n != 4000 {
		t.Fatalf("revealed %d runes, want 4000", n)
	}
}

// Revealing must not split a multi-byte rune into a replacement box.
func TestRevealKeepsRunesIntact(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	m.liveQueue = strings.Repeat("é", 40)
	for m.revealLive() {
	}
	if m.liveQueue != "" || !strings.HasPrefix(m.live, "éé") {
		t.Fatalf("rune boundary broken: %q", m.live)
	}
}

// The tick drops to a frame only while a reply is revealing, and the
// spinner must not spin up with it.
func TestTickIntervalFollowsTheReveal(t *testing.T) {
	if got := tickInterval(true, true); got != revealFrame {
		t.Fatalf("revealing tick = %v, want %v", got, revealFrame)
	}
	if got := tickInterval(false, true); got != spinInterval {
		t.Fatalf("busy tick = %v, want %v", got, spinInterval)
	}
	if got := tickInterval(false, false); got != idleTick {
		t.Fatalf("idle tick = %v, want %v", got, idleTick)
	}

	m := sizeModel(t, testModel())
	m.state = stRunning
	m.lastSpin = time.Now()
	before := m.spinnerFrame
	m.liveQueue = "text to reveal"
	m.testUpdate(tickMsg(time.Now()))
	if m.spinnerFrame != before {
		t.Fatalf("spinner advanced on a reveal frame: %d -> %d", before, m.spinnerFrame)
	}
	if m.lastSpin.IsZero() {
		t.Fatal("precondition: spinner should have a reference time")
	}
}

// The reveal is only worth anything if each frame paints. Advancing the
// buffer without repainting would leave the reader watching the old
// cadence with extra state attached.
func TestRevealPaintsEveryFrame(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	previous := 0
	for _, chunk := range []string{"al", "pha", "be", "ta"} {
		m.testUpdate(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": chunk}}))
		next, _ := m.testUpdate(tickMsg(time.Now()))
		mm := next.(*Model)
		view := strings.TrimSpace(ansi.Strip(mm.transcript.Viewport.View()))
		if !strings.HasSuffix(view, strings.TrimSpace(mm.live)) {
			t.Fatalf("frame did not paint the revealed text: live=%q view=%q", mm.live, view)
		}
		if len(mm.live) <= previous {
			t.Fatalf("frame revealed nothing: %d then %d", previous, len(mm.live))
		}
		previous = len(mm.live)
	}
}

func TestLiveCapped(t *testing.T) {
	m := sizeModel(t, testModel())
	big := strings.Repeat("z", liveMaxRunes+10)
	updated, _ := m.testUpdate(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": big}}))
	mm := updated.(*Model)
	if n := len([]rune(mm.live)); n <= liveMaxRunes {
		t.Fatalf("live = %d runes, want over cap with marker", n)
	}
	if !mm.liveCut || !strings.Contains(mm.live, "live truncated") {
		t.Fatal("cap must mark once")
	}
	before := mm.live
	updated, _ = mm.testUpdate(eventMsg(events.Event{Name: "delta", Fields: map[string]any{"text": "more"}}))
	if mm := updated.(*Model); mm.live != before {
		t.Fatal("capped buffer must drop further chunks")
	}
}

func TestThinkExpandsGlobally(t *testing.T) {
	m := sizeModel(t, testModel())
	m.appendBlock(thinkBlock("old deliberation " + strings.Repeat("x", 200)))
	m.appendBlock(thinkBlock("new deliberation"))
	m.transcript.compact = false
	m.refreshContent()
	view := m.transcript.Viewport.View()
	// Reflow wraps long runs: count runes, not substrings.
	if n := strings.Count(view, "x"); n < 200 {
		t.Fatalf("global expand opened %d x-runes, want 200", n)
	}
	if n := strings.Count(view, "[THINKING]"); n != 2 {
		t.Fatalf("both thinks must label expanded: %d headers", n)
	}
}
