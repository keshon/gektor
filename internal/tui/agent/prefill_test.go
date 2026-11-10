package agent

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
)

func TestCompactionProgressAppearsAndClears(t *testing.T) {
	m := sizeModelTo(t, testModel(), 120, 34)
	m.state = stRunning
	for _, generated := range []int{0, 42} {
		m.handleEvent(events.Event{Name: "progress", Fields: map[string]any{"phase": "checkpoint", "completion": generated}})
		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, "COMPACT") || !strings.Contains(view, "Summarizing earlier steps") {
			t.Fatal(view)
		}
		if generated > 0 && !strings.Contains(view, "42 tokens generated") {
			t.Fatal(view)
		}
		if len(m.transcript.blocks) != 0 {
			t.Fatal("progress was added to saved history")
		}
	}
	m.hideSYS = true
	m.refreshContent()
	if strings.Contains(ansi.Strip(m.View().Content), "Summarizing earlier steps") {
		t.Fatal("system toggle did not hide compaction")
	}
	m.hideSYS = false
	m.handleEvent(events.Event{Name: "progress", Fields: map[string]any{"phase": "generating"}})
	if strings.Contains(ansi.Strip(m.View().Content), "Summarizing earlier steps") {
		t.Fatal("compaction message survived phase change")
	}
}

func TestPrefillSeparatesCacheFromNewWork(t *testing.T) {
	for _, tc := range []struct {
		name   string
		p      llm.Progress
		want   string
		filled int
	}{
		{"partial", llm.Progress{PromptTokens: 1000, ProcessedTokens: 800, CachedTokens: 600, CacheKnown: true}, "Cached 600 · Processing 200/400", 40},
		{"zero", llm.Progress{PromptTokens: 1000, ProcessedTokens: 800, CacheKnown: true}, "Cached 0 · Processing 800/1000", 64},
		{"unknown", llm.Progress{PromptTokens: 1000, ProcessedTokens: 800}, "Cached unknown · Prompt 800/1000", 64},
		{"all cached", llm.Progress{PromptTokens: 1000, ProcessedTokens: 1000, CachedTokens: 1000, CacheKnown: true}, "No new input to process", 80},
		{"invalid cache", llm.Progress{PromptTokens: 1000, ProcessedTokens: 800, CachedTokens: 1200, CacheKnown: true}, "Cached unknown", 64},
		{"empty", llm.Progress{}, "Cached unknown", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := ansi.Strip(prefillText(tc.p, defaultStyles(), 80))
			if !strings.Contains(text, tc.want) || strings.Count(text, "━") != tc.filled {
				t.Fatal(text)
			}
		})
	}
}

func TestCompactionBarDistinguishesMeasuredAndUnknownProgress(t *testing.T) {
	p := llm.Progress{Phase: "checkpoint", Subphase: "prefill", PromptTokens: 1000, ProcessedTokens: 800, CachedTokens: 600, CacheKnown: true}
	st := defaultStyles()
	first := historyProgressText(p, st, 80, 0)
	if !strings.HasPrefix(ansi.Strip(first), "COMPACT\n") || strings.Count(ansi.Strip(first), "━") != 40 {
		t.Fatal(first)
	}
	if first != historyProgressText(p, st, 80, 1) {
		t.Fatal("measured bar animated without progress")
	}
	p.Subphase = "generating"
	first = historyProgressText(p, st, 80, 0)
	if first == historyProgressText(p, st, 80, 1) || strings.Contains(ansi.Strip(first), "Processing 200/400") {
		t.Fatal("generation must use an indeterminate bar")
	}
}

func TestUnifiedHeaderKeepsActivityAndContext(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := sizeModelTo(t, testModel(), width, 24)
		m.state = stRunning
		m.tokens, m.limit = 56600, 65536
		m.progress.Phase = "checkpoint"
		text := ansi.Strip(m.badgeLine())
		if !strings.Contains(text, "COMPACT  56.6k/65.5k") {
			t.Fatal(width, text)
		}
		for _, b := range m.headerBadges() {
			if b.id == "context" {
				t.Fatal("duplicate context badge")
			}
		}
		m.spinnerFrame++
		if text == ansi.Strip(m.badgeLine()) {
			t.Fatal("busy indicator did not animate")
		}
		m.state = stDone
		text = ansi.Strip(m.badgeLine())
		m.spinnerFrame++
		if text != ansi.Strip(m.badgeLine()) {
			t.Fatal("idle indicator animated")
		}
	}
}
