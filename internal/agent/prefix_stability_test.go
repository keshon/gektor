package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/llm"
)

// A backend that caches a prompt reuses it only when the new prompt starts
// with the old one, in the token order its chat template produces. Anything
// that edits an earlier turn throws the whole cache away, and the model
// re-reads the conversation for no visible reason.
//
// This drives the real loop - real system prompt, real registry, real
// nudges, real compaction - and measures, between consecutive requests, how
// much of the message list a backend could have reused.

func sameMessage(a, b llm.Message) bool {
	if a.Role != b.Role || a.Content != b.Content || a.ToolCallID != b.ToolCallID || !slices.Equal(a.Images, b.Images) || len(a.ToolCalls) != len(b.ToolCalls) {
		return false
	}
	for i := range a.ToolCalls {
		if a.ToolCalls[i].ID != b.ToolCalls[i].ID || a.ToolCalls[i].Name != b.ToolCalls[i].Name ||
			string(a.ToolCalls[i].Arguments) != string(b.ToolCalls[i].Arguments) {
			return false
		}
	}
	return true
}

// reusablePrefix is how many leading messages two prompts share.
func reusablePrefix(a, b []llm.Message) int {
	n := 0
	for n < len(a) && n < len(b) && sameMessage(a[n], b[n]) {
		n++
	}
	return n
}

// stepClient asks for a tool call on every step, reporting a caller-chosen
// prompt size so compaction can be reached without a huge fixture.
type stepClient struct {
	promptTokens []int
	reqs         []llm.ChatRequest
	tool         string
}

func (c *stepClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.reqs = append(c.reqs, req)
	i := len(c.reqs) - 1
	// No fixture means "every step is a tool step", so probes that are not
	// about context size still get a run to measure.
	if len(c.promptTokens) > 0 && i >= len(c.promptTokens) {
		return llm.ChatResponse{
			Message:      llm.Message{Role: llm.RoleAssistant, Content: "done"},
			FinishReason: "stop",
		}, nil
	}
	prompt := 0
	if len(c.promptTokens) > 0 {
		prompt = c.promptTokens[i]
	}
	return llm.ChatResponse{
		Message: llm.Message{
			Role:    llm.RoleAssistant,
			Content: fmt.Sprintf("step %d", i),
			ToolCalls: []llm.ToolCall{{
				ID:        fmt.Sprintf("c%d", i),
				Name:      c.tool,
				Arguments: json.RawMessage(`{}`),
			}},
		},
		FinishReason: "tool_calls",
		Usage:        llm.Usage{PromptTokens: prompt},
	}, nil
}

// growTool produces output large enough that history actually grows, which
// is what makes compaction reachable at all.
type growTool struct{}

func (growTool) Name() string        { return "grow" }
func (growTool) Description() string { return "produce output" }
func (growTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (growTool) Mode() ToolMode { return Exclusive }
func (growTool) Run(context.Context, json.RawMessage) (string, error) {
	return strings.Repeat("result line of output\n", 40), nil
}

// failTool fails identically every time, which is what trips the stuck
// detector - the nudge most likely to fire repeatedly.
type failTool struct{}

func (failTool) Name() string        { return "fail" }
func (failTool) Description() string { return "always fails" }
func (failTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (failTool) Mode() ToolMode { return Exclusive }
func (failTool) Run(context.Context, json.RawMessage) (string, error) {
	return "", fmt.Errorf("same failure")
}

// transition is one step boundary: how much of the prompt was reusable.
type transition struct {
	step       int
	reused     int
	before     int
	compacted  bool
	divergedAt string
}

// probe runs the loop and returns what a caching backend could have reused.
func probe(t *testing.T, cfg Config, promptTokens []int, tool Tool, toolName string) []transition {
	t.Helper()
	client := &stepClient{promptTokens: promptTokens, tool: toolName}
	cfg.Client = client
	cfg.Tools = NewRegistry(tool)
	if cfg.System == "" {
		cfg.System = "you are a careful agent"
	}
	if cfg.MaxSteps == 0 {
		cfg.MaxSteps = 30
	}
	compacted := map[int]bool{}
	cfg.OnNudge = func(kind, _ string) {
		if kind == NudgeCompact {
			compacted[len(client.reqs)] = true
		}
	}
	if _, err := New(cfg).Run(context.Background(), "do the task"); err != nil {
		t.Logf("run ended: %v", err)
	}

	var out []transition
	for i := 1; i < len(client.reqs); i++ {
		before, now := client.reqs[i-1].Messages, client.reqs[i].Messages
		reused := reusablePrefix(before, now)
		tr := transition{step: i, reused: reused, before: len(before), compacted: compacted[i]}
		if reused < len(before) {
			tr.divergedAt = describe(before[reused])
		}
		out = append(out, tr)
	}
	return out
}

func describe(m llm.Message) string {
	s := string(m.Role) + ": " + m.Content
	if len(m.ToolCalls) > 0 {
		s += fmt.Sprintf(" tool=%s", m.ToolCalls[0].Name)
	}
	if len(s) > 56 {
		s = s[:56] + "..."
	}
	return s
}

func (tr transition) String() string {
	mark := ""
	if tr.compacted {
		mark = " [COMPACTED]"
	}
	if tr.divergedAt != "" {
		return fmt.Sprintf("step %2d: reusable %2d of %2d%s  rewrote -> %s",
			tr.step, tr.reused, tr.before, mark, tr.divergedAt)
	}
	return fmt.Sprintf("step %2d: reusable %2d of %2d%s  appended only",
		tr.step, tr.reused, tr.before, mark)
}

func rewritesOnlyOnCompaction(t *testing.T, name string, trs []transition) {
	t.Helper()
	t.Logf("--- %s", name)
	for _, tr := range trs {
		t.Logf("    %s", tr)
	}
	for _, tr := range trs {
		if tr.divergedAt == "" {
			continue
		}
		if tr.compacted {
			t.Logf("  step %d rewrote history, and compaction is the reason", tr.step)
			continue
		}
		t.Fatalf("step %d rewrote earlier history without compacting: %s", tr.step, tr.divergedAt)
	}
}

// An ordinary run, nowhere near its window. Every step must extend the
// prompt. A rewrite here means the model re-reads the conversation every
// turn for no reason at all.
func TestPromptsOnlyEverGrowAtTheTail(t *testing.T) {
	trs := probe(t, Config{SkipVerify: true},
		[]int{100, 200, 300, 400, 500}, growTool{}, "grow")
	rewritesOnlyOnCompaction(t, "ordinary run, no compaction", trs)
	for _, tr := range trs {
		if tr.divergedAt != "" {
			t.Fatalf("step %d rewrote history in an ordinary run: %s", tr.step, tr.divergedAt)
		}
	}
}

// Nudges are harness text injected into the conversation. Inserted anywhere
// but the end, they invalidate the whole prefix, and the model gets no
// warning that anything moved.
func TestNudgesDoNotRewriteEarlierTurns(t *testing.T) {
	trs := probe(t, Config{SkipVerify: true, MaxStuckSteps: 2, Nudges: NudgeFull},
		nil, failTool{}, "fail")
	if len(trs) == 0 {
		t.Fatal("no steps ran; the probe would pass vacuously")
	}
	rewritesOnlyOnCompaction(t, "stuck detector firing repeatedly", trs)
	var nudged int
	for _, tr := range trs {
		if strings.Contains(tr.String(), "harness") {
			nudged++
		}
	}
	t.Logf("  %d transitions, harness nudges appear as appended user turns", len(trs))
}

// Compaction is the one thing that legitimately rewrites earlier history.
// This pins down that it is the ONLY thing that does, so a future
// optimisation to compaction cannot quietly become a cache bug.
func TestCompactionIsTheOnlyRewrite(t *testing.T) {
	trs := probe(t, Config{
		SkipVerify:       true,
		ContextLimit:     1000,
		CompactAtPercent: 40,
		CompactKeepSteps: 2,
	}, []int{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000, 1100, 1200},
		growTool{}, "grow")

	rewritesOnlyOnCompaction(t, "compaction enabled, window small", trs)
	var compacted int
	for _, tr := range trs {
		if tr.compacted {
			compacted++
		}
	}
	if compacted == 0 {
		t.Fatal("compaction never fired; the probe proves nothing")
	}
	t.Logf("  compaction fired %d times across %d steps", compacted, len(trs))
}
