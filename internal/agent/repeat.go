package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
)

// maxRepeatedSteps is how many consecutive steps may issue the same set of
// tool calls before the loop stops offering tools and asks for an answer.
//
// Two is too eager: a model that lists a directory and then reads the files
// it named repeats its first signature once before it has enough to answer,
// and cutting it there produces a thinner answer than the one it was about to
// give. Three lets one genuinely useful pass through and still catches a
// model that has stopped learning - every repeat after that adds context and
// answers nothing.
const maxRepeatedSteps = 3

// ErrRepeatingSteps marks a run ended because the model repeated an identical
// set of tool calls instead of answering. It is separate from ErrMaxSteps for
// the same reason ErrMaxSteps is separate from transport failure: the run did
// work, so the budget is not what ran out.
var ErrRepeatingSteps = errors.New("model repeated identical steps")

// callKey identifies one tool call for repeat detection, canonicalizing the
// arguments so key order and whitespace do not read as a different call.
func callKey(name string, args json.RawMessage) string {
	var obj map[string]any
	if err := json.Unmarshal(args, &obj); err != nil {
		return name + ":" + string(args)
	}
	canonical, err := json.Marshal(obj)
	if err != nil {
		return name + ":" + string(args)
	}
	return name + ":" + string(canonical)
}

// callSignature identifies a whole step. Order within a step is irrelevant -
// parallel calls are compared as a set - so reordering the same calls cannot
// look like progress. The separator is a byte JSON cannot contain raw, so two
// different sets of calls cannot join into the same string.
func callSignature(calls []llm.ToolCall) string {
	parts := make([]string, len(calls))
	for i, c := range calls {
		parts[i] = callKey(c.Name, c.Arguments)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x00")
}

// trackSignature advances the repeat counter and returns how many consecutive
// steps have now issued this signature.
func (st *runState) trackSignature(signature string) int {
	if st.repeatCount > 0 && signature == st.repeatSignature {
		st.repeatCount++
		return st.repeatCount
	}
	st.repeatSignature, st.repeatCount = signature, 1
	return 1
}

// noteMutation forgets both repeat caches. The same read can now legitimately
// return something new, and a signature that repeats across a mutation is work
// in progress (test, patch, test) rather than a spin - which is why the
// counter, not just the read cache, has to go.
func (st *runState) noteMutation() {
	st.forgetIdempotentSeen()
	st.repeatSignature, st.repeatCount = "", 0
}

// forgetIdempotentSeen drops the record of which pure reads already ran. The
// refusal text promises the result "is still valid - re-read it from the
// conversation", which stops being true once a mutation supersedes it or
// compaction drops that conversation from the prompt.
func (st *runState) forgetIdempotentSeen() { st.idempotentSeen = nil }

// knownTools is the set of tool names the model was offered, for the leak
// parser: text shaped like a call to an unknown tool is prose, not a call.
func (a *Agent) knownTools() map[string]bool {
	known := map[string]bool{}
	if a.cfg.Tools != nil {
		for _, n := range a.cfg.Tools.Names() {
			known[n] = true
		}
	}
	return known
}

// forceAnswer ends a run that has stopped making progress with an answer
// instead of an error: the model gets one turn with no tools at all, so the
// only thing it can produce is text. Withholding the tools is what makes this
// deterministic - the nudge it replaces could be, and was, ignored, so a soft
// "stop repeating yourself" would only be one more line to ignore.
//
// from and step bracket the run of identical steps that triggered it, for the
// error text and the report warning.
func (a *Agent) forceAnswer(ctx context.Context, st *runState, step int, history []llm.Message, from int) (string, error) {
	history = a.nudge(history, NudgeRepeating, prompts.RepeatingSteps)
	a.saveState(history)
	resp, err := a.chat(ctx, llm.ChatRequest{
		Messages:  history,
		MaxTokens: a.effectiveMaxTokens(st.projectPrompt(llm.EstimatePromptTokens(llm.ChatRequest{Messages: history}))),
	})
	// The answer turn is a model round-trip like any other; counting it keeps
	// Steps honest for whoever reads the report.
	a.report.Steps = step + 2
	if err != nil {
		return "", fmt.Errorf("steps %d-%d repeated identically, and the answer turn failed: %w", from, step, err)
	}
	// No tools were offered, but a model can still write a call as text. Strip
	// it so what reaches the answer is prose; the recovered calls are dropped
	// rather than executed, since this turn exists to stop acting.
	_, cleaned, leaked := llm.ExtractLeakedCalls(resp.Message.Content, a.knownTools())
	resp.Message.Content = cleaned
	resp.Message.ToolCalls = nil
	history = append(history, resp.Message)
	if a.cfg.OnStep != nil {
		a.cfg.OnStep(step+1, resp.Message)
	}
	if a.cfg.OnUsage != nil {
		a.cfg.OnUsage(step+1, resp.Usage)
	}
	if resp.Usage.PromptTokens > 0 {
		st.lastPromptTokens = resp.Usage.PromptTokens
		a.report.LastPromptTokens = st.lastPromptTokens
	}
	a.saveState(history)
	out := a.handleFinish(st, resp, leaked, history)
	if !out.done {
		return "", fmt.Errorf("%w: steps %d-%d issued identical calls and the tool-free turn produced no answer",
			ErrRepeatingSteps, from, step)
	}
	return out.answer, out.err
}
