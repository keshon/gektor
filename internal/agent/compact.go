package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/prompts"
	"github.com/keshon/tars/internal/session"
)

// compactHistory drops older assistant-led step groups from history while
// keeping the system prompt, original user task, and the most recent
// keepSteps groups intact. Tool results are never separated from their
// matching assistant tool_call. The inserted notice carries an extractive
// summary of what was dropped (step count, tools used, paths touched) so
// the model keeps ground facts instead of just being told text is gone.
func compactHistory(history []llm.Message, keepSteps int) []llm.Message {
	if keepSteps <= 0 || len(history) <= 2 {
		return history
	}

	prefix := append([]llm.Message(nil), history[:2]...)
	rest := history[2:]
	groups := stepGroups(rest)
	if len(groups) <= keepSteps {
		return history
	}

	cut := findCutPoint(groups, len(groups)-keepSteps)
	dropped := groups[:cut]
	kept := groups[cut:]

	out := append([]llm.Message(nil), prefix...)
	out = append(out, llm.Message{Role: llm.RoleUser, Content: harnessText(compactNotice(dropped))})
	for _, g := range kept {
		out = append(out, g...)
	}
	return out
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

// compactNotice renders the insertion: the base notice plus a mechanical
// summary of dropped groups. Extractive on purpose — no model call, so it
// cannot fail and costs nothing on a weak backend.
func compactNotice(dropped [][]llm.Message) string {
	summary := summarizeDropped(dropped)
	if summary == "" {
		return prompts.CompactNotice
	}
	return prompts.CompactNotice + "\n" + summary
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

// pruneMinimumTokens stops compaction from firing on small windows: below
// this many prompt tokens there is nothing worth summarizing and the churn
// costs more than it saves.
const pruneMinimumTokens = 20000

// Context budgets: the token math that decides generation sizes and
// warning thresholds. Sibling to compaction (which reacts when the
// window fills) rather than part of it.

// effectiveMaxTokens caps Config.MaxTokens against the room actually left
// in the context window, using the prompt size from the previous call as
// a stand-in for "about how big the next prompt will be" (history only
// grows a little per step, so this is a safe approximation, not the exact
// next value). Without this, asking for e.g. 32768 tokens of generation
// when the prompt itself is already using real context space causes
// exactly what koboldcpp warns about: "most of the context will be
// removed" — the backend starts evicting the prompt mid-generation and
// produces incoherent, runaway output instead of erroring cleanly.
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
	if room < 256 {
		room = 256 // always ask for *something* rather than zero/negative
	}
	if room > a.cfg.MaxTokens {
		return a.cfg.MaxTokens
	}
	return room
}

// budgetWarning returns a one-time nudge when usage crosses a new context
// threshold, or "" if there's nothing new to report. *warned tracks the
// highest percentage already warned about, so crossing 75% doesn't nag
// every single step afterward — only the next, higher threshold matters.
func (a *Agent) budgetWarning(usage llm.Usage, warned *int) string {
	if a.cfg.ContextLimit <= 0 || usage.PromptTokens <= 0 {
		return ""
	}
	pct := usage.PromptTokens * 100 / a.cfg.ContextLimit
	msg := ""
	switch {
	case pct >= 90 && *warned < 90:
		*warned = 90
		msg = fmt.Sprintf(prompts.BudgetWarning, usage.PromptTokens, a.cfg.ContextLimit, pct)
	case pct >= 75 && *warned < 75:
		*warned = 75
		msg = fmt.Sprintf(prompts.BudgetNotice, usage.PromptTokens, a.cfg.ContextLimit, pct)
	default:
		return ""
	}
	return msg + a.hint("compact")
}

// defaultCompactAtPercent is the share of the context window at which
// history is compacted. Well below the old 90%: a window is what the
// backend will accept, not what the model can still reason over, and the
// steps taken between those two points are the ones that produce
// confidently wrong work.
const defaultCompactAtPercent = 60

// dropRepeatCaches forgets which calls ran and which failed. The refusal
// texts cite conversation content ("re-read it from the conversation",
// "already failed identically") — true while that content is present,
// false once compaction drops it or a mutation supersedes it. The maps
// re-learn from fresh executions, bounded exactly as before.
func dropRepeatCaches(st *runState) {
	st.idempotentSeen = nil
	st.failedCalls = nil
}

// CompactFile mechanically compacts a saved snapshot in place: the
// same compactHistory the loop uses, invoked on demand (/compact)
// instead of at the usage threshold. Returns message counts before
// and after; writes only when something actually dropped, atomically
// like saveState. An error leaves the snapshot untouched.
func CompactFile(stateFile string, keepSteps int) (before, after int, err error) {
	history, err := session.ReadHistory(stateFile)
	if err != nil {
		return 0, 0, err
	}
	before = len(history)
	compacted := compactHistory(history, keepSteps)
	after = len(compacted)
	if after >= before {
		return before, after, nil
	}
	data, err := json.MarshalIndent(compacted, "", "  ")
	if err != nil {
		return before, after, err
	}
	tmp := stateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return before, after, err
	}
	if err := os.Rename(tmp, stateFile); err != nil {
		return before, after, err
	}
	return before, after, nil
}

// maybeCompact drops old step groups once the prompt passes the
// threshold, and may do so more than once in a run — a long run that
// compacted at step 12 and then grew again is in exactly the state
// compaction exists for.
//
// compactHistory returns its input unchanged when there is nothing left
// to drop, which is what stops a full history from being compacted every
// step to no effect.
func (a *Agent) maybeCompact(history *[]llm.Message, usage llm.Usage, st *runState) bool {
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
	compacted := compactHistory(*history, a.cfg.CompactKeepSteps)
	if len(compacted) >= len(*history) {
		return false
	}
	*history = compacted
	dropRepeatCaches(st)
	return true
}

// summarizeDropped extracts ground facts from dropped step groups: how many
// steps, which tools ran, which paths were touched, what the user asked
// for, what the plan was, and the last assistant text snippet. Bounded
// output — this string itself lives in context.
//
// The dropped user requests and the plan are the load-bearing part. Without
// them a compaction that keeps the first user message as the standing task
// leaves the model holding an already-answered aside and no checklist: it
// finishes the work still visible in the tail, runs out, and answers a
// question from twenty steps ago.
func summarizeDropped(dropped [][]llm.Message) string {
	if len(dropped) == 0 {
		return ""
	}
	toolCounts := map[string]int{}
	var paths []string
	var asks []string
	lastText := ""
	for _, g := range dropped {
		for _, m := range g {
			if m.Role == llm.RoleAssistant {
				for _, tc := range m.ToolCalls {
					toolCounts[tc.Name]++
					for _, p := range mutatedPathsFromCall(tc.Arguments) {
						if p != "" && !containsStr(paths, p) && len(paths) < 20 {
							paths = append(paths, p)
						}
					}
				}
				if strings.TrimSpace(m.Content) != "" {
					lastText = strings.TrimSpace(m.Content)
				}
			}
			if m.Role == llm.RoleUser {
				if a := droppedAsk(m.Content); a != "" && !containsStr(asks, a) && len(asks) < 8 {
					asks = append(asks, a)
				}
			}
			if m.Role == llm.RoleTool {
				// Delegate reports name no files in their call
				// arguments — the paths live in the result envelope.
				// Without this, subagent work compacts down to a bare
				// delegate_task×N with no record of what it touched.
				_, dps := parseDelegateMutations(m.Content)
				for _, p := range dps {
					if p != "" && !containsStr(paths, p) && len(paths) < 20 {
						paths = append(paths, p)
					}
				}
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "(dropped %d earlier steps:", len(dropped))
	names := make([]string, 0, len(toolCounts))
	for n := range toolCounts {
		names = append(names, n)
	}
	sort.Strings(names)
	for i, n := range names {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, " %s×%d", n, toolCounts[n])
	}
	if len(paths) > 0 {
		fmt.Fprintf(&b, "; files: %s", strings.Join(paths, ", "))
	}
	b.WriteString(")")
	if len(asks) > 0 {
		fmt.Fprintf(&b, " User requests dropped: %s", quoteEach(asks))
	}
	if plan := droppedPlan(dropped); plan != "" {
		fmt.Fprintf(&b, " Your plan at that point (keep working it): %s", plan)
	}
	if lastText != "" {
		r := []rune(lastText)
		if len(r) > 200 {
			lastText = string(r[:200]) + "…"
		}
		fmt.Fprintf(&b, " Last note: %q", lastText)
	}
	return b.String()
}

// droppedAsk returns a user's dropped request as one summary line, or ""
// when the message is a loop notice rather than something they asked for.
// Harness notices describe the loop, not the task, and a wall of them
// would crowd out the request that matters.
func droppedAsk(content string) string {
	t := strings.TrimSpace(content)
	if t == "" || strings.HasPrefix(t, harnessPrefix) {
		return ""
	}
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	if r := []rune(t); len(r) > 160 {
		t = string(r[:160]) + "…"
	}
	return t
}

// droppedPlan returns the checklist carried by the most recent dropped
// checklist call, as one summary line, or "" when the dropped steps held
// none.
//
// The plan lives only in the last checklist call's arguments: rehydrateTodo
// rebuilds the tracker by replaying exactly that call. So a compaction that
// drops it does not lose steps, it loses the model's own task list, and the
// notice was reporting only "todo×2".
//
// Matched by argument shape rather than tool name because the summary is a
// pure function over messages with no registry. The shape is narrow — an
// items array whose entries carry id, state and text, with state from a
// closed set — and todo is the only tool that takes one.
func droppedPlan(dropped [][]llm.Message) string {
	for i := len(dropped) - 1; i >= 0; i-- {
		for _, m := range dropped[i] {
			for _, tc := range m.ToolCalls {
				if plan, ok := formatChecklist(tc.Arguments); ok {
					return plan
				}
			}
		}
	}
	return ""
}

// planBounds keep the plan readable and cheap in context: this string is
// carried for the rest of the run, not just until the next compaction.
const (
	planMaxItems = 12
	planItemMax  = 60
)

// formatChecklist renders a checklist tool call's arguments as
// "[x] done / [~] in progress / [ ] pending". ok is false when the
// arguments are not a checklist.
func formatChecklist(args json.RawMessage) (string, bool) {
	var in struct {
		Items []struct {
			State string `json:"state"`
			Text  string `json:"text"`
		} `json:"items"`
	}
	if err := json.Unmarshal(args, &in); err != nil || len(in.Items) == 0 {
		return "", false
	}
	marks := map[string]string{"done": "x", "in_progress": "~", "pending": " "}
	parts := make([]string, 0, len(in.Items))
	for i, it := range in.Items {
		mark, known := marks[it.State]
		if !known || strings.TrimSpace(it.Text) == "" {
			return "", false // not a checklist after all
		}
		if i == planMaxItems {
			parts = append(parts, fmt.Sprintf("+%d more", len(in.Items)-planMaxItems))
			break
		}
		text := strings.TrimSpace(it.Text)
		if r := []rune(text); len(r) > planItemMax {
			text = string(r[:planItemMax]) + "…"
		}
		parts = append(parts, fmt.Sprintf("[%s] %s", mark, text))
	}
	return strings.Join(parts, "; "), true
}

func quoteEach(items []string) string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = strconv.Quote(s)
	}
	return strings.Join(out, "; ")
}
