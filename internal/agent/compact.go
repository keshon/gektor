package agent

import (
	"encoding/json"
	"fmt"
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

// notice renders the compaction notice: the base prompt plus the carried
// summary. Extractive on purpose — no model call, so it cannot fail and
// costs nothing on a weak backend.
func (f droppedFacts) notice() string {
	body := prompts.CompactNotice
	if summary := f.render(); summary != "" {
		body += "\n" + summary
	}
	return harnessText(body)
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

// defaultCompactAtPercent is how full the window may get before the loop
// drops old step groups. The remaining share is headroom for the next step,
// not for the answer: completionBudget already clamps generation to whatever
// room is left, so the only thing that can overflow is a step that grows the
// prompt with tool output. At 60% a 262k window compacted while a hundred
// thousand tokens were still free, dropping context the model could have
// used; 85% keeps the work and still leaves room for several large results.
const defaultCompactAtPercent = 85

// CompactFile mechanically compacts a saved snapshot in place: the
// same compactHistory the loop uses, invoked on demand (/compact)
// instead of at the usage threshold. Writes only when something actually
// dropped, through the same atomic writer as saveState. An error leaves the
// snapshot untouched.
//
// The compacted history is returned rather than left for the caller to read
// back: it was already decoded here, and the /compact path read the same
// file a second time purely to get it.
func CompactFile(stateFile string, keepSteps int) (before, after int, history []llm.Message, err error) {
	history, err = session.ReadHistory(stateFile)
	if err != nil {
		return 0, 0, nil, err
	}
	before = len(history)
	compacted := compactHistory(history, keepSteps)
	after = len(compacted)
	if after >= before {
		return before, after, history, nil
	}
	if err := session.WriteSnapshot(stateFile, compacted); err != nil {
		return before, after, history, err
	}
	return before, after, compacted, nil
}

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
	// Still over the line from the last pass, and not enough new work has
	// happened for another pass to free anything the first one could not.
	if st.lastCompactTokens > 0 && usage.PromptTokens < st.lastCompactTokens+a.compactionCooldown() {
		return false
	}
	compacted := compactHistory(*history, a.cfg.CompactKeepSteps)
	if len(compacted) >= len(*history) {
		return false
	}
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

// droppedFacts is what a compaction carries forward about the turns it
// dropped. It exists so the next compaction can merge into these instead
// of rebuilding from a window that no longer contains them.
//
// Every field is bounded: this string lives in context for the rest of the
// run, so it must never become the largest thing in the prompt.
type droppedFacts struct {
	steps    int
	tools    map[string]int
	paths    []string
	asks     []string
	plan     string
	lastText string
}

const (
	factMaxPaths = 20
	factMaxAsks  = 8
	factMaxNote  = 200
)

// Line tags. The summary is tagged and line-oriented so the next pass can
// read it back; a bare prose blob can only be re-derived from messages
// that compaction has already removed.
const (
	tagSteps = "dropped "
	tagPaths = "files: "
	tagAsks  = "asks: "
	tagPlan  = "plan: "
	tagNote  = "note: "
)

func newFacts() droppedFacts { return droppedFacts{tools: map[string]int{}} }

// add extracts ground facts from dropped step groups: how many steps, which
// tools ran, which paths were touched, what the user asked for, what the
// plan was, and the last assistant text snippet.
//
// The dropped user requests and the plan are the load-bearing part. Without
// them a compaction that keeps the first user message as the standing task
// leaves the model holding an already-answered aside and no checklist: it
// finishes the work still visible in the tail, runs out, and answers a
// question from twenty steps ago.
//
// Order matters: asks append and the plan overwrites, because the carried
// set is already older than this drop.
func (f *droppedFacts) add(dropped [][]llm.Message) {
	if len(dropped) == 0 {
		return
	}
	if f.tools == nil {
		f.tools = map[string]int{}
	}
	f.steps += len(dropped)
	for _, g := range dropped {
		for _, m := range g {
			switch m.Role {
			case llm.RoleAssistant:
				for _, tc := range m.ToolCalls {
					f.tools[tc.Name]++
					for _, p := range mutatedPathsFromCall(tc.Arguments) {
						f.addPath(p)
					}
				}
				if t := strings.TrimSpace(m.Content); t != "" {
					f.lastText = t
				}
			case llm.RoleUser:
				if a := droppedAsk(m.Content); a != "" && !containsStr(f.asks, a) && len(f.asks) < factMaxAsks {
					f.asks = append(f.asks, a)
				}
			case llm.RoleTool:
				// Delegate reports name no files in their call
				// arguments — the paths live in the result envelope.
				// Without this, subagent work compacts down to a bare
				// delegate_task×N with no record of what it touched.
				_, dps := parseDelegateMutations(m.Content)
				for _, p := range dps {
					f.addPath(p)
				}
			}
		}
	}
	if p := droppedPlan(dropped); p != "" {
		f.plan = p
	}
}

func (f *droppedFacts) addPath(p string) {
	if p != "" && !containsStr(f.paths, p) && len(f.paths) < factMaxPaths {
		f.paths = append(f.paths, p)
	}
}

// render writes the facts as the tagged block that both the model reads
// and the next compaction parses back.
func (f droppedFacts) render() string {
	if f.steps == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s%d earlier steps:", tagSteps, f.steps)
	names := make([]string, 0, len(f.tools))
	for n := range f.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	for i, n := range names {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, " %s×%d", n, f.tools[n])
	}
	if len(f.paths) > 0 {
		fmt.Fprintf(&b, "\n%s%s", tagPaths, strings.Join(f.paths, ", "))
	}
	if len(f.asks) > 0 {
		fmt.Fprintf(&b, "\n%s%s", tagAsks, quoteEach(f.asks))
	}
	if f.plan != "" {
		fmt.Fprintf(&b, "\n%s%s", tagPlan, f.plan)
	}
	if f.lastText != "" {
		note := f.lastText
		if r := []rune(note); len(r) > factMaxNote {
			note = string(r[:factMaxNote]) + "…"
		}
		fmt.Fprintf(&b, "\n%s%q", tagNote, note)
	}
	return b.String()
}

// parseFacts reads back a rendered block. ok is false unless a steps line
// is present, which is what distinguishes a compaction notice from any
// other harness message.
func parseFacts(body string) (droppedFacts, bool) {
	f := newFacts()
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, tagSteps):
			rest, ok := strings.CutPrefix(line, tagSteps)
			count, tools, ok := strings.Cut(rest, " earlier steps:")
			if !ok {
				return f, false
			}
			n, err := strconv.Atoi(strings.TrimSpace(count))
			if err != nil {
				return f, false
			}
			f.steps = n
			for _, entry := range strings.Split(tools, ",") {
				name, count, ok := strings.Cut(strings.TrimSpace(entry), "×")
				if !ok || name == "" {
					continue
				}
				c, err := strconv.Atoi(count)
				if err != nil {
					continue
				}
				f.tools[name] += c
			}
		case strings.HasPrefix(line, tagPaths):
			f.paths = splitAndTrim(strings.TrimPrefix(line, tagPaths))
		case strings.HasPrefix(line, tagAsks):
			f.asks = unquoteEach(strings.TrimPrefix(line, tagAsks))
		case strings.HasPrefix(line, tagPlan):
			f.plan = strings.TrimPrefix(line, tagPlan)
		case strings.HasPrefix(line, tagNote):
			if note, err := strconv.Unquote(strings.TrimPrefix(line, tagNote)); err == nil {
				f.lastText = note
			}
		}
	}
	return f, f.steps > 0
}

func splitAndTrim(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// unquoteEach reverses quoteEach, which is how the asks were written.
func unquoteEach(s string) []string {
	var out []string
	for _, part := range strings.Split(s, "; ") {
		if q, err := strconv.Unquote(strings.TrimSpace(part)); err == nil && q != "" {
			out = append(out, q)
		}
	}
	return out
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
