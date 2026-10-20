package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

// countingWrite is an ordinary mutating tool: Exclusive, and it records that
// it ran.
type countingWrite struct{ runs *int32 }

func (countingWrite) Name() string            { return "write_file" }
func (countingWrite) Description() string     { return "stub write" }
func (countingWrite) Mode() ToolMode          { return Exclusive }
func (countingWrite) Idempotent() bool        { return false }
func (countingWrite) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t countingWrite) Run(context.Context, json.RawMessage) (string, error) {
	atomic.AddInt32(t.runs, 1)
	return "written", nil
}

func countText(msgs []llm.Message, text string) int {
	n := 0
	for _, m := range msgs {
		if m.Content == text {
			n++
		}
	}
	return n
}

// A stance belongs at the tail: it is a harness turn, not part of the system
// prompt, so switching it cannot rewrite the prompt prefix.
func TestRun_AppendsTheStanceAtTheTail(t *testing.T) {
	client := &repeatSeqClient{responses: []llm.ChatResponse{say("done")}}
	a := New(Config{
		Client: client, Tools: NewRegistry(), System: "sys", Directive: "PLAN",
	})

	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	msgs := client.reqs[0].Messages
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want system, task and stance", len(msgs))
	}
	if msgs[0].Content != "sys" {
		t.Fatalf("system prompt = %q, want it untouched", msgs[0].Content)
	}
	if want := harnessPrefix + "PLAN"; msgs[2].Content != want {
		t.Fatalf("last message = %q, want %q", msgs[2].Content, want)
	}
}

// The stance is idempotent: resuming in the same role must not stack copies.
func TestResume_KeepsOneCopyOfTheStance(t *testing.T) {
	client := &repeatSeqClient{responses: []llm.ChatResponse{say("done"), say("more"), say("more")}}
	plan := New(Config{Client: client, Tools: NewRegistry(), System: "sys", Directive: "PLAN"})

	if _, err := plan.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	history := client.reqs[0].Messages
	if _, err := plan.Resume(context.Background(), history, "continue"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	resumed := client.reqs[1].Messages
	if got := countText(resumed, harnessPrefix+"PLAN"); got != 1 {
		t.Fatalf("plan stance appears %d times after resume, want 1", got)
	}
}

// Switching to the acting role must countermand the plan stance with a turn
// of its own; otherwise the last mode instruction in the conversation still
// says "change nothing".
func TestResume_ActingRoleCountermandsThePlanStance(t *testing.T) {
	client := &repeatSeqClient{responses: []llm.ChatResponse{say("plan"), say("done"), say("done")}}
	plan := New(Config{Client: client, Tools: NewRegistry(), System: "sys", Directive: "PLAN"})
	if _, err := plan.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	history := client.reqs[0].Messages

	act := New(Config{
		Client: client, Tools: NewRegistry(), System: "sys",
		Directive: "ACT", Supersedes: "PLAN",
	})
	if _, err := act.Resume(context.Background(), history, "do it"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	switched := client.reqs[1].Messages
	last := switched[len(switched)-1]
	if want := harnessPrefix + "ACT"; last.Content != want {
		t.Fatalf("last message = %q, want %q", last.Content, want)
	}
	if _, err := act.Resume(context.Background(), switched, "again"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got := countText(client.reqs[2].Messages, harnessPrefix+"ACT"); got != 1 {
		t.Fatalf("act stance appears %d times, want 1", got)
	}
}

// The whole point of the design: changing the stance appends, so a backend
// that caches the prompt keeps everything it already read.
func TestModeSwitchAppendsInsteadOfRewriting(t *testing.T) {
	client := &repeatSeqClient{responses: []llm.ChatResponse{
		stepOf(readCall("1", "a.txt")),
		say("the plan"),
		say("done"),
	}}
	var runs int32
	a := New(Config{
		Client: client, Tools: NewRegistry(repeatingReadTool{runs: &runs}), System: "sys",
		Directive: "PLAN", ReadOnly: []string{"read_file"},
	})
	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	planMessages := client.reqs[len(client.reqs)-1].Messages

	act := New(Config{
		Client: client, Tools: NewRegistry(repeatingReadTool{runs: &runs}), System: "sys",
		Directive: "ACT", Supersedes: "PLAN",
	})
	if _, err := act.Resume(context.Background(), planMessages, "go"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	actMessages := client.reqs[len(client.reqs)-1].Messages

	if got := reusablePrefix(planMessages, actMessages); got != len(planMessages) {
		t.Fatalf("reusable prefix = %d of %d: the switch rewrote earlier history",
			got, len(planMessages))
	}
}

// A read-only role still advertises the full tool set, so a mode switch
// cannot change the prompt prefix. Refusal is what removes the capability.
func TestReadOnlyRoleRefusesMutations(t *testing.T) {
	var reads, writes int32
	a := New(Config{
		Tools:    NewRegistry(repeatingReadTool{runs: &reads}, countingWrite{runs: &writes}),
		ReadOnly: []string{"read_file"},
	})
	if !strings.Contains(strings.Join(a.cfg.Tools.Names(), ","), "write_file") {
		t.Fatalf("write_file missing from the advertised set: %v", a.cfg.Tools.Names())
	}

	out := a.executeToolCalls(context.Background(), &runState{}, 0, "stop",
		[]llm.ToolCall{readCall("1", "a.txt"), writeCall("2", "a.txt")}, nil)

	if reads != 1 {
		t.Fatalf("read ran %d times, want 1", reads)
	}
	if writes != 0 {
		t.Fatalf("write ran %d times, want it refused", writes)
	}
	refusal := out[len(out)-1].Content
	if !strings.Contains(refusal, "read-only") || !strings.Contains(refusal, "write_file") {
		t.Fatalf("refusal = %q, want it to name the tool and the mode", refusal)
	}
	if !strings.Contains(out[len(out)-2].Content, "contents") {
		t.Fatalf("read result = %q, want it to run", out[len(out)-2].Content)
	}
}

// Compaction may drop the stance along with the turns it summarises; the loop
// puts it back rather than continuing without it.
func TestCompactionRestoresTheStance(t *testing.T) {
	for _, stance := range []string{"PLAN", "ACT"} {
		t.Run(stance, func(t *testing.T) {
			steps := historyWithSteps(10)
			history := append([]llm.Message(nil), steps[:2]...)
			history = append(history, llm.Message{Role: llm.RoleUser, Content: harnessPrefix + stance})
			history = append(history, steps[2:]...)
			a := New(Config{
				Client: &stubClient{}, Tools: NewRegistry(), System: "sys", Directive: stance,
				ContextLimit: 1000, CompactKeepSteps: 2, CompactAtPercent: 10,
			})
			if !a.maybeCompact(&history, llm.Usage{PromptTokens: 500}, &runState{}) {
				t.Fatal("compaction did not fire")
			}
			for _, msg := range history {
				if msg.Content == harnessPrefix+stance {
					t.Fatal("test did not drop the original stance")
				}
			}
			history = a.withDirective(history)
			if last := history[len(history)-1]; last.Content != harnessPrefix+stance {
				t.Fatalf("last message = %q, want the stance restored at the tail", last.Content)
			}
		})
	}
}

// A large window is there to be used: compacting at 60% threw away context
// while a hundred thousand tokens were still free.
func TestDefaultCompactionLeavesTheWindowUsable(t *testing.T) {
	a := New(Config{
		Client: &stubClient{}, Tools: NewRegistry(), System: "sys",
		ContextLimit: 100000, CompactKeepSteps: 2,
	})
	if got := a.cfg.CompactAtPercent; got != 85 {
		t.Fatalf("default CompactAtPercent = %d, want 85", got)
	}
	history := historyWithSteps(10)
	if a.maybeCompact(&history, llm.Usage{PromptTokens: 60000}, &runState{}) {
		t.Error("compacted at 60% of a 100k window")
	}
	if !a.maybeCompact(&history, llm.Usage{PromptTokens: 85000}, &runState{}) {
		t.Error("did not compact at 85%")
	}
}
