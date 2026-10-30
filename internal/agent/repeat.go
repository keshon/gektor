package agent

import (
	"context"
	"crypto/sha256"
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

// noteMutation clears repetition after a potentially mutating action.
func (st *runState) noteMutation() {
	st.repeatSignature, st.repeatCount = "", 0
	st.observations = nil
}

// Diagnostic only: polling and useful rereads can legitimately repeat results.
func (a *Agent) noteObservation(st *runState, name string, args json.RawMessage, output string) {
	signature := sha256.Sum256([]byte(callKey(name, args) + "\x00" + output))
	st.observations = append(st.observations, signature)
	if len(st.observations) > 8 {
		st.observations = st.observations[1:]
	}
	duplicates := 0
	seen := map[[32]byte]bool{}
	for _, item := range st.observations {
		if seen[item] {
			duplicates++
		}
		seen[item] = true
	}
	if duplicates >= 4 && !st.observationWarning {
		st.observationWarning = true
		a.report.Warnings = append(a.report.Warnings, "Repeated tool observations in the last 8 calls; inspect for polling or investigation without new evidence. Diagnostic only; execution was not stopped.")
	}
}

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
	projected := a.reuseContext(history)
	resp, err := a.chat(ctx, llm.ChatRequest{
		Messages:  projected,
		MaxTokens: a.effectiveMaxTokens(st.projectPrompt(llm.EstimatePromptTokens(llm.ChatRequest{Messages: projected}))),
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
