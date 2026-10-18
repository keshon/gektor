package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/llm"
)

// A backend re-reads the whole prompt when the prefix changes. The loop
// only ever appends, which keeps the prefix warm — except compaction,
// which rewrites history from index 2. So the question is not whether a
// compaction invalidates the cache (it must) but how often one fires.

// longHistory builds history whose step groups are big enough that keeping
// the window still leaves the prompt above the threshold. That is the case
// where "compact every step" and "compaction works" collide.
func longHistory(groups, bodyChars int) []llm.Message {
	h := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "task"},
	}
	for i := 0; i < groups; i++ {
		h = append(h,
			llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprintf("step %d %s", i, strings.Repeat("x", bodyChars))},
			llm.Message{Role: llm.RoleUser, Content: "continue"},
		)
	}
	return h
}

// The stable prefix is what a backend can reuse between steps.
func stablePrefix(a, b []llm.Message) int {
	n := 0
	for n < len(a) && n < len(b) && a[n].Role == b[n].Role && a[n].Content == b[n].Content {
		n++
	}
	return n
}

func TestCompactionFrequencyWhenTheWindowStillOverflows(t *testing.T) {
	// 1000-token window, compact at 50%. Groups are large, so dropping to
	// the keep window does not bring the prompt back under the line.
	a := New(Config{
		Client: &stubClient{}, Tools: NewRegistry(), System: "sys",
		ContextLimit: 1000, CompactKeepSteps: 8, CompactAtPercent: 50,
	})
	history := longHistory(40, 400)
	var st runState

	fires, steps := 0, 0
	prefixAtFire := []int{}
	prev := history
	for steps < 12 {
		steps++
		// A step's worth of growth: a tool round trip.
		history = append(history,
			llm.Message{Role: llm.RoleAssistant, Content: "more " + strings.Repeat("y", 400)},
			llm.Message{Role: llm.RoleTool, ToolCallID: "c", Content: "ok"},
		)
		// Prompt size roughly proportional to the history we hold.
		usage := llm.Usage{PromptTokens: len(history) * 12}
		if a.maybeCompact(&history, usage, &st) {
			fires++
			prefixAtFire = append(prefixAtFire, stablePrefix(prev, history))
		}
		prev = append([]llm.Message(nil), history...)
	}
	t.Logf("compacted %d times in %d steps", fires, steps)
	for i, p := range prefixAtFire {
		t.Logf("  fire %d: stable prefix = %d messages of %d", i+1, p, len(history))
	}
	if fires >= steps {
		t.Fatalf("compaction fires every step (%d of %d)", fires, steps)
	}
}
