package agent

import (
	"errors"
	"testing"

	"github.com/keshon/tars/internal/llm"
)

// Compaction rewrites history from the notice onward, which makes a caching
// backend re-read the whole prompt. The threshold alone made that a
// per-step cost for any run whose keep-window sits above the line: each
// pass dropped one group, the next step added one back, and the notice's
// own step count changed, so the prompt never settled.

func TestCompactionWaitsForMeaningfulGrowthBeforePayingAgain(t *testing.T) {
	a := New(Config{
		Client: &stubClient{}, Tools: NewRegistry(), System: "sys",
		ContextLimit: 100000, CompactKeepSteps: 2, CompactAtPercent: 10,
	})
	history := longHistory(40, 400)
	var st runState

	// Over the threshold, so the first pass runs.
	if !a.maybeCompact(&history, llm.Usage{PromptTokens: 20000}, &st) {
		t.Fatal("setup: the first pass over the threshold did not run")
	}
	trigger := 20000

	// Just past the line, and barely more than the last pass saw. Another
	// pass here frees nothing the first one did not already take, so it is
	// not worth the re-read even though there are groups to drop again.
	for _, tokens := range []int{trigger + 1, trigger + 100, trigger + 999} {
		if a.maybeCompact(&history, llm.Usage{PromptTokens: tokens}, &st) {
			t.Fatalf("compacted again at %d tokens, only %d past the last pass",
				tokens, tokens-trigger)
		}
	}

	// Enough new work to make another pass worth its re-read. History has to
	// have grown too, or there is simply nothing left to drop.
	history = append(history, longHistory(4, 400)[2:]...)
	if !a.maybeCompact(&history, llm.Usage{PromptTokens: trigger + a.compactionCooldown()}, &st) {
		t.Fatalf("did not compact after %d tokens of new work", a.compactionCooldown())
	}
}

// The cooldown must not outlive the run it was for. A fresh pass resets it,
// or a long run that compacts early would stop compacting altogether.
func TestCompactionCooldownResetsOnEachPass(t *testing.T) {
	a := New(Config{
		Client: &stubClient{}, Tools: NewRegistry(), System: "sys",
		ContextLimit: 100000, CompactKeepSteps: 2, CompactAtPercent: 10,
	})
	var st runState
	first := 20000
	history := longHistory(40, 400)
	if !a.maybeCompact(&history, llm.Usage{PromptTokens: first}, &st) {
		t.Fatal("setup: did not compact")
	}
	second := first + a.compactionCooldown()
	history = longHistory(40, 400)
	if !a.maybeCompact(&history, llm.Usage{PromptTokens: second}, &st) {
		t.Fatal("the second pass did not run")
	}
	// The cooldown is measured from the newest pass, not the first.
	history = longHistory(40, 400)
	if a.maybeCompact(&history, llm.Usage{PromptTokens: second + 1}, &st) {
		t.Fatal("the cooldown was still measured from the first pass")
	}
	history = longHistory(40, 400)
	if !a.maybeCompact(&history, llm.Usage{PromptTokens: second + a.compactionCooldown()}, &st) {
		t.Fatal("the third pass did not run")
	}
}

// Overflow recovery is the backstop the cooldown leans on: if the prompt
// really does not fit, compaction must happen regardless of when the last
// pass ran.
func TestOverflowRecoveryIgnoresTheCooldown(t *testing.T) {
	a := New(Config{
		Client: &stubClient{}, Tools: NewRegistry(), System: "sys",
		ContextLimit: 100000, CompactKeepSteps: 2, CompactAtPercent: 10,
	})
	history := longHistory(40, 400)
	var st runState

	// Put the cooldown in the "recently compacted" state.
	st.lastCompactTokens = 90000

	next, err := a.handleChatError(&st, 1, errors.New("maximum context length exceeded"), history)
	if err != nil {
		t.Fatalf("overflow should recover, got %v", err)
	}
	if next == nil {
		return // recovered by refusing the step; either way the run continued
	}
	if len(next) >= len(history) {
		t.Fatalf("overflow did not compact: %d messages, started with %d", len(next), len(history))
	}
}
