package agent

import (
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

// A compaction the observer cannot see leaves the transcript holding turns
// the model can no longer read, with nothing saying where they went. The
// notice lives in the model's history, so nothing else would ever show it.
func TestMaybeCompactReportsTheDrop(t *testing.T) {
	var seen []string
	a := New(Config{
		ContextLimit:     1000,
		CompactKeepSteps: 2,
		CompactAtPercent: 10,
		OnNudge:          func(kind, text string) { seen = append(seen, kind+": "+text) },
	})
	var history []llm.Message
	history = append(history, llm.Message{Role: llm.RoleSystem, Content: "sys"})
	history = append(history, llm.Message{Role: llm.RoleUser, Content: "task"})
	for i := 0; i < 6; i++ {
		history = append(history,
			llm.Message{Role: llm.RoleAssistant, Content: "step " + string(rune('a'+i))},
			llm.Message{Role: llm.RoleUser, Content: "also " + string(rune('a'+i))},
		)
	}
	var st runState
	if !a.maybeCompact(&history, llm.Usage{PromptTokens: 500}, &st) {
		t.Fatal("did not compact")
	}
	if len(seen) != 1 {
		t.Fatalf("observer heard %d notices, want 1: %v", len(seen), seen)
	}
	if !strings.HasPrefix(seen[0], NudgeCompact+": ") {
		t.Fatalf("notice kind = %q, want %q", seen[0], NudgeCompact)
	}
	// The reported text is the notice the model itself received, so the
	// two cannot describe different drops.
	if !strings.Contains(seen[0], "earlier steps") {
		t.Fatalf("notice did not describe what was dropped: %q", seen[0])
	}
	if !strings.Contains(history[2].Content, "earlier steps") {
		t.Fatalf("model history and observer report disagree:\n%s\n%s", history[2].Content, seen[0])
	}
}

func TestMaybeCompactStaysQuietWhenNothingDrops(t *testing.T) {
	var seen []string
	a := New(Config{
		ContextLimit:     1000,
		CompactKeepSteps: 8,
		CompactAtPercent: 10,
		OnNudge:          func(kind, text string) { seen = append(seen, kind) },
	})
	history := historyWithSteps(3)
	var st runState
	if a.maybeCompact(&history, llm.Usage{PromptTokens: 500}, &st) {
		t.Fatal("compacted a history shorter than the keep window")
	}
	if len(seen) != 0 {
		t.Fatalf("reported a drop that did not happen: %v", seen)
	}
}

// CompactKeepSteps -1 is documented as disabling threshold compaction. It
// must actually do so, or a setting that says "off" is a lie.
func TestNegativeKeepStepsDisablesThresholdCompaction(t *testing.T) {
	a := New(Config{ContextLimit: 1000, CompactKeepSteps: -1, CompactAtPercent: 10})
	history := historyWithSteps(20)
	before := len(history)
	var st runState
	if a.maybeCompact(&history, llm.Usage{PromptTokens: 900}, &st) {
		t.Fatal("compaction ran with compaction disabled")
	}
	if len(history) != before {
		t.Fatalf("history changed: %d -> %d", before, len(history))
	}
}
