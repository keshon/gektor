package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
)

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

// droppedFacts is what a compaction carries forward about the turns it
// dropped. It exists so the next compaction can merge into these instead
// of rebuilding from a window that no longer contains them.
//
// Every field is bounded: this string lives in context for the rest of the
// run, so it must never become the largest thing in the prompt.
type droppedFacts struct {
	steps      int
	tools      map[string]int
	paths      []string
	asks       []string
	plan       string
	lastText   string
	checkpoint string
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
	tagSteps      = "dropped "
	tagPaths      = "files: "
	tagAsks       = "asks: "
	tagPlan       = "plan: "
	tagNote       = "note: "
	tagCheckpoint = "checkpoint: "
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
				if a := droppedAsk(m.Content); a != "" {
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
	if f.checkpoint != "" {
		fmt.Fprintf(&b, "\n%s%q", tagCheckpoint, f.checkpoint)
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
			rest := strings.TrimPrefix(line, tagSteps)
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
		case strings.HasPrefix(line, tagCheckpoint):
			f.checkpoint, _ = strconv.Unquote(strings.TrimPrefix(line, tagCheckpoint))
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
	if json.Unmarshal([]byte(s), &out) == nil {
		return out
	}
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
	data, _ := json.Marshal(items)
	return string(data)
}
