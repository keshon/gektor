package agent

import (
	"strings"

	"github.com/keshon/gektor/internal/llm"
)

// compactHistory drops older assistant-led step groups from history while
// keeping the system prompt, original user task, and the most recent
// keepSteps groups intact. Tool results are never separated from their
// matching assistant tool_call. The inserted notice carries an extractive
// summary of what was dropped (step count, tools used, paths touched) so
// the model keeps ground facts instead of just being told text is gone.
//
// The notice is cumulative. Compaction exists for runs that outlive one
// pass, and a summary rebuilt from each pass's own window describes only
// that window: the previous notice is lifted back out, its facts merged
// forward, and the merged set re-emitted. Without that the plan and the
// dropped requests survive exactly one pass.
func compactHistory(history []llm.Message, keepSteps int) []llm.Message {
	if keepSteps <= 0 || len(history) <= 2 {
		return history
	}

	prefix := append([]llm.Message(nil), history[:2]...)
	// Lift our own notice out of the tail first. It is a user-role
	// message, so stepGroups would count it as a kept step: every pass
	// would then drop it to make room, compact on every step, and
	// regenerate the summary from a window that no longer held any of
	// the older work.
	carried, rest := splitNotice(history[2:])
	groups := stepGroups(rest)
	if len(groups) <= keepSteps {
		return history
	}

	cut := findCutPoint(groups, len(groups)-keepSteps)
	kept := groups[cut:]

	facts := carried
	facts.add(groups[:cut])
	// User instructions remain complete. If they alone exceed the bounded
	// projection, refuse to compact rather than silently discard constraints.
	if len(facts.notice()) > 64*1024 {
		return history
	}

	out := append([]llm.Message(nil), prefix...)
	out = append(out, llm.Message{Role: llm.RoleUser, Content: facts.notice()})
	for _, g := range kept {
		out = append(out, g...)
	}
	return out
}

// splitNotice lifts a carried-over compaction notice out of the tail,
// returning its facts and the remaining messages.
//
// Only a message at that exact position, that parses as a compaction
// notice, is lifted. Other harness messages exist — stuck, budget and
// think-wrap notices are all user-role and carry the same prefix — and
// mistaking one for a summary would both delete the advice and import
// nothing.
func splitNotice(rest []llm.Message) (droppedFacts, []llm.Message) {
	if len(rest) == 0 {
		return newFacts(), rest
	}
	body := strings.TrimPrefix(rest[0].Content, harnessPrefix)
	if body == rest[0].Content {
		return newFacts(), rest
	}
	facts, ok := parseFacts(body)
	if !ok {
		return newFacts(), rest
	}
	return facts, rest[1:]
}

// findCutPoint returns how many leading groups to drop so exactly want
// groups are dropped, adjusted so the cut never orphans a tool result:
// groups are assistant-led by construction, but a lone user turn forms
// its own group, and a defensive scan backs the cut off if kept[0] would
// start with a tool message.
//
// The scan tests for RoleTool, not for "not assistant". Backing off from
// any non-assistant group means a session where the user interleaves
// messages with the model's work can never compact at all: the cut walks
// back to zero, dropped comes out empty, and the caller is handed a
// "compacted" history identical to the one it passed in.
func findCutPoint(groups [][]llm.Message, want int) int {
	if want < 0 {
		want = 0
	}
	if want > len(groups) {
		want = len(groups)
	}
	for want > 0 && want < len(groups) && len(groups[want]) > 0 &&
		groups[want][0].Role == llm.RoleTool {
		want--
	}
	return want
}

// stepGroups splits messages into assistant-led steps: each group starts
// with an assistant message and includes every following non-assistant
// message until the next assistant turn.
func stepGroups(msgs []llm.Message) [][]llm.Message {
	var groups [][]llm.Message
	i := 0
	for i < len(msgs) {
		start := i
		i++
		for i < len(msgs) && msgs[i].Role != llm.RoleAssistant {
			i++
		}
		groups = append(groups, msgs[start:i])
	}
	return groups
}
