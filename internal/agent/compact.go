package agent

import (
	"context"
	"strings"

	"github.com/keshon/gektor/internal/llm"
)

// pruneMinimumTokens stops compaction from firing on small windows: below
// this many prompt tokens there is nothing worth summarizing and the churn
// costs more than it saves.
const pruneMinimumTokens = 20000

// projectPrompt anchors the estimate to the last provider count and adds
// newly appended content. It is still approximate for an unknown tokenizer.
func (st *runState) projectPrompt(estimate int) int {
	if st.lastPromptEstimate > 0 && st.lastPromptTokens > 0 {
		return max(1, st.lastPromptTokens+estimate-st.lastPromptEstimate)
	}
	return estimate
}

// effectiveMaxTokens reserves space against the current projected input.
func (a *Agent) effectiveMaxTokens(lastPromptTokens int) int {
	if a.cfg.ContextLimit <= 0 {
		return a.cfg.MaxTokens
	}
	promptEstimate := lastPromptTokens
	if promptEstimate <= 0 {
		// First call, nothing measured yet. System prompt + tool schemas
		// alone are routinely 1000+ tokens (we've seen 1135 in practice)
		// — don't assume zero just because we haven't measured it.
		promptEstimate = 1536
	}
	const safetyMargin = 256 // chat template / role overhead, not exact
	room := a.cfg.ContextLimit - promptEstimate - safetyMargin
	if room < 1 {
		room = 1 // the backend reports an overflow if even this cannot fit
	}
	if room > a.cfg.MaxTokens {
		return a.cfg.MaxTokens
	}
	return room
}

// The threshold is a share of the full input+output context window. Input
// is projected before sending; the remaining output budget is capped separately.
const defaultCompactAtPercent = 85

// compactionCooldown is how much the prompt must grow past the size that
// triggered the last compaction before another threshold pass is worth its
// price. A share of the window, floored so a small window is not re-read
// every single step.
//
// Compaction rewrites history from the notice onward, so a backend that
// caches prompts re-reads everything. Without a cooldown, a run whose
// keep-window alone sits above the threshold compacts on every step
// forever: each pass drops one group and the next step adds one back, the
// notice's own step count changes, and the prompt prefix changes with it.
// Measured against a real loop, that turned 9 of 12 steps into full
// re-reads. The fix is not to compact less eagerly but to stop paying for a
// pass that cannot help: if one pass left us above the line, the next will
// too, because the only difference is one step of work.
//
// Overflow recovery (handleChatError) is unaffected and still compacts hard
// and immediately, so being late to the threshold costs a warning-free
// steady state rather than an overflow.
const compactionCooldownNumerator = 20

func (a *Agent) compactionCooldown() int {
	return max(a.cfg.ContextLimit/compactionCooldownNumerator, 512)
}

// maybeCompact drops old step groups once the prompt passes the
// threshold, and may do so more than once in a run — a long run that
// compacted at step 12 and then grew again is in exactly the state
// compaction exists for. How often it may do so is bounded by
// compactionCooldown.
//
// compactHistory returns its input unchanged when there is nothing left
// to drop, which is what stops a full history from being compacted every
// step to no effect.
func (a *Agent) maybeCompact(history *[]llm.Message, usage llm.Usage, st *runState, contexts ...context.Context) bool {
	if a.cfg.ContextLimit <= 0 || a.cfg.CompactKeepSteps <= 0 || usage.PromptTokens <= 0 {
		return false
	}
	if usage.PromptTokens*100/a.cfg.ContextLimit < a.cfg.CompactAtPercent {
		return false
	}
	// Small windows compact on percent alone; large windows also require
	// enough absolute tokens to be worth the churn.
	if a.cfg.ContextLimit >= 32000 && usage.PromptTokens < pruneMinimumTokens {
		return false
	}
	// Still over the line from the last pass, and not enough new work has
	// happened for another pass to free anything the first one could not.
	if st.lastCompactTokens > 0 && usage.PromptTokens < a.cfg.ContextLimit-256 && usage.PromptTokens < st.lastCompactTokens+a.compactionCooldown() {
		return false
	}
	compacted := compactHistory(*history, a.cfg.CompactKeepSteps)
	// A few large results can exceed the window before eight groups exist.
	// Reduce the retained tail only when its estimate is already at the limit.
	inputBudget := a.cfg.ContextLimit - 256
	if a.cfg.ContextLimit >= 2048 {
		inputBudget = a.cfg.ContextLimit - min(a.cfg.MaxTokens, max(1024, a.cfg.ContextLimit/4)) - 512
	}
	for keep := a.cfg.CompactKeepSteps / 2; keep > 0 && llm.EstimatePromptTokens(a.modelRequest(compacted)) > inputBudget; keep /= 2 {
		compacted = compactHistory(*history, keep)
	}
	if len(compacted) >= len(*history) {
		return false
	}
	a.captureHistory(*history)
	var archiveErr error
	compacted, archiveErr = retainArchivedInstructions(a.snapshot, compacted)
	if archiveErr != nil {
		st.compactErr = archiveErr
		return false
	}
	if a.cfg.Summarize {
		ctx := context.Background()
		if len(contexts) > 0 {
			ctx = contexts[0]
		}
		var err error
		compacted, err = a.checkpoint(ctx, *history, compacted)
		if err != nil {
			st.compactErr = err
			return false
		}
		compacted, err = a.fitCheckpoint(compacted, inputBudget)
		if err != nil {
			st.compactErr = err
			return false
		}
	}
	a.compactContext(*history, compacted)
	*history = compacted
	st.lastCompactTokens = usage.PromptTokens
	// Report the compaction. The notice lives in the model's history, so
	// without this an observer sees a transcript holding turns the model can
	// no longer read, with nothing saying where they went. The text is the
	// notice itself rather than a second rendering of it, so the two cannot
	// disagree.
	if len(compacted) > 2 && strings.HasPrefix(compacted[2].Content, harnessPrefix) {
		a.notifyNudge(NudgeCompact, compacted[2].Content)
	}
	return true
}
