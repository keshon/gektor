package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

// repeatingReadTool is a pure read: Concurrent, declaring itself idempotent.
type repeatingReadTool struct{ runs *int32 }

func (repeatingReadTool) Name() string        { return "read_file" }
func (repeatingReadTool) Description() string { return "stub read" }
func (repeatingReadTool) Mode() ToolMode      { return Concurrent }
func (repeatingReadTool) Idempotent() bool    { return true }
func (repeatingReadTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t repeatingReadTool) Run(context.Context, json.RawMessage) (string, error) {
	atomic.AddInt32(t.runs, 1)
	return "contents", nil
}

// repeatingPlainTool reads too, but never claims purity: it must not be cached
// and must not be refused on repeat.
type repeatingPlainTool struct{ runs *int32 }

func (repeatingPlainTool) Name() string        { return "plain_file" }
func (repeatingPlainTool) Description() string { return "stub" }
func (repeatingPlainTool) Mode() ToolMode      { return Concurrent }
func (repeatingPlainTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t repeatingPlainTool) Run(context.Context, json.RawMessage) (string, error) {
	atomic.AddInt32(t.runs, 1)
	return "contents", nil
}

func readCall(id, path string) llm.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path})
	return llm.ToolCall{ID: id, Name: "read_file", Arguments: args}
}

func writeCall(id, path string) llm.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path})
	return llm.ToolCall{ID: id, Name: "write_file", Arguments: args}
}

// stepOf makes a one-call assistant response.
func stepOf(call llm.ToolCall) llm.ChatResponse {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}}}
}

// repeatSeqClient serves scripted responses and keeps every request, so a test
// can assert what the model was offered on the answer turn.
type repeatSeqClient struct {
	responses []llm.ChatResponse
	reqs      []llm.ChatRequest
}

func (c *repeatSeqClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.reqs = append(c.reqs, req)
	return c.responses[len(c.reqs)-1], nil
}

func TestCallSignature_IgnoresCallOrder(t *testing.T) {
	a := readCall("1", "a.txt")
	b := readCall("2", "b.txt")
	one, other := callSignature([]llm.ToolCall{a, b}), callSignature([]llm.ToolCall{b, a})
	if one != other {
		t.Fatalf("signatures differ by order: %q vs %q", one, other)
	}
	if one == callSignature([]llm.ToolCall{a}) {
		t.Fatal("a subset produced the same signature as the whole set")
	}
}

// An exact repeat of a pure read is refused rather than served the same bytes
// a second time: the bytes cannot have changed, and the duplicate would only
// consume context.
func TestExecuteToolCalls_RefusesExactIdempotentRepeat(t *testing.T) {
	var runs int32
	a := New(Config{Tools: NewRegistry(repeatingReadTool{runs: &runs})})
	st := &runState{}
	ctx := context.Background()

	first := a.executeToolCalls(ctx, st, 0, "stop", []llm.ToolCall{readCall("1", "a.txt")}, nil)
	if runs != 1 {
		t.Fatalf("first call ran %d times, want 1", runs)
	}
	if got := first[len(first)-1].Content; !strings.Contains(got, "contents") {
		t.Fatalf("first result = %q, want the tool output", got)
	}

	second := a.executeToolCalls(ctx, st, 1, "stop", []llm.ToolCall{readCall("2", "a.txt")}, nil)
	if runs != 1 {
		t.Fatalf("repeat executed the tool (runs=%d), want it refused", runs)
	}
	got := second[len(second)-1].Content
	if !strings.Contains(got, "already ran in step 0") || !strings.Contains(got, "re-read it from the conversation") {
		t.Fatalf("refusal = %q, want it to name the step and point at the history", got)
	}
}

// A mutation makes the same read a legitimate question again, so the refusal
// cache has to be dropped with it.
func TestExecuteToolCalls_RepeatAfterMutationRunsAgain(t *testing.T) {
	var runs int32
	a := New(Config{Tools: NewRegistry(repeatingReadTool{runs: &runs}, mutStub{"write_file"})})
	st := &runState{}
	ctx := context.Background()

	a.executeToolCalls(ctx, st, 0, "stop", []llm.ToolCall{readCall("1", "a.txt")}, nil)
	a.executeToolCalls(ctx, st, 1, "stop", []llm.ToolCall{writeCall("2", "a.txt")}, nil)
	out := a.executeToolCalls(ctx, st, 2, "stop", []llm.ToolCall{readCall("3", "a.txt")}, nil)

	if runs != 2 {
		t.Fatalf("post-mutation read ran %d times, want 2", runs)
	}
	if got := out[len(out)-1].Content; !strings.Contains(got, "contents") {
		t.Fatalf("post-mutation read = %q, want the tool output", got)
	}
}

// A tool that does not opt in is never cached: its result may be time-varying.
func TestExecuteToolCalls_NonIdempotentToolRunsEveryTime(t *testing.T) {
	var runs int32
	a := New(Config{Tools: NewRegistry(repeatingPlainTool{runs: &runs})})
	st := &runState{}
	ctx := context.Background()
	call := llm.ToolCall{ID: "1", Name: "plain_file", Arguments: json.RawMessage(`{"path":"a.txt"}`)}

	a.executeToolCalls(ctx, st, 0, "stop", []llm.ToolCall{call}, nil)
	a.executeToolCalls(ctx, st, 1, "stop", []llm.ToolCall{call}, nil)
	if runs != 2 {
		t.Fatalf("non-idempotent tool ran %d times, want 2", runs)
	}
}

// Three identical steps is a spin. Instead of erroring, the loop offers no
// tools at all for one turn: with nothing to call, text is all the model can
// produce.
func TestRun_RepeatedStepsAnswerWithoutTools(t *testing.T) {
	var runs int32
	client := &repeatSeqClient{responses: []llm.ChatResponse{
		stepOf(readCall("1", "a.txt")),
		stepOf(readCall("2", "a.txt")),
		stepOf(readCall("3", "a.txt")),
		say("the answer"),
	}}
	var nudgeKinds []string
	a := New(Config{
		Client: client,
		Tools:  NewRegistry(repeatingReadTool{runs: &runs}),
		System: "sys",
		OnNudge: func(kind, _ string) {
			nudgeKinds = append(nudgeKinds, kind)
		},
	})

	answer, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "the answer" {
		t.Fatalf("answer = %q, want the tool-free turn's text", answer)
	}
	if client.reqs[3].Tools != nil {
		t.Fatalf("answer turn offered %d tools, want none", len(client.reqs[3].Tools))
	}
	if got := a.Report().Steps; got != 4 {
		t.Fatalf("Steps = %d, want 4 (three tool steps and the answer turn)", got)
	}
	warnings := strings.Join(a.Report().Warnings, " | ")
	if !strings.Contains(warnings, "steps 0-2 issued identical tool calls") {
		t.Fatalf("Warnings = %q, want the repeated-step range", warnings)
	}
	if len(nudgeKinds) != 1 || nudgeKinds[0] != NudgeRepeating {
		t.Fatalf("nudges = %v, want one %q", nudgeKinds, NudgeRepeating)
	}
	history := client.reqs[3].Messages
	last := history[len(history)-1]
	if last.Role != llm.RoleUser || !strings.HasPrefix(last.Content, harnessPrefix) {
		t.Fatalf("answer turn history ends with %+v, want the marked harness line", last)
	}
}

func TestRepeatAnswerReachesEventsAndSnapshot(t *testing.T) {
	var runs int32
	c := &repeatSeqClient{responses: []llm.ChatResponse{stepOf(readCall("1", "a")), stepOf(readCall("2", "a")), stepOf(readCall("3", "a")), say("final answer")}}
	c.responses[3].Usage = llm.Usage{PromptTokens: 321, CompletionTokens: 9}
	var steps, usage int
	path := filepath.Join(t.TempDir(), "state.json")
	a := New(Config{Client: c, Tools: NewRegistry(repeatingReadTool{&runs}), StateFile: path, OnStep: func(_ int, m llm.Message) {
		if m.Content == "final answer" {
			steps++
		}
	}, OnUsage: func(_ int, u llm.Usage) {
		if u.PromptTokens == 321 {
			usage++
		}
	}})
	if _, err := a.Run(t.Context(), "task"); err != nil {
		t.Fatal(err)
	}
	h, err := LoadState(path)
	if err != nil || h[len(h)-1].Content != "final answer" || steps != 1 || usage != 1 || a.Report().LastPromptTokens != 321 {
		t.Fatalf("history=%+v steps=%d usage=%d report=%+v err=%v", h, steps, usage, a.Report(), err)
	}
}

type failingMutation struct{ auditTool }

func (failingMutation) Run(context.Context, json.RawMessage) (string, error) {
	return "partial edit", errors.New("exit 1")
}

func TestReadsAfterMutationBarrierUseFreshResult(t *testing.T) {
	for _, failed := range []bool{false, true} {
		var reads int32
		ran := false
		var writer Tool = auditTool{&ran}
		if failed {
			writer = failingMutation{}
		}
		a := New(Config{Tools: NewRegistry(repeatingReadTool{&reads}, writer)})
		st := &runState{}
		a.executeToolCalls(t.Context(), st, 0, "stop", []llm.ToolCall{readCall("1", "a")}, nil)
		out := a.executeToolCalls(t.Context(), st, 1, "stop", []llm.ToolCall{{ID: "w", Name: "audit_tool", Arguments: json.RawMessage(`{}`)}, readCall("2", "a")}, nil)
		if reads != 2 || strings.HasPrefix(out[len(out)-1].Content, "error:") {
			t.Fatalf("failed=%v reads=%d out=%+v", failed, reads, out)
		}
	}
}

// Repeating a signature is not a spin when it mutates in between: the same
// test/patch pair running three times is work, not a loop.
func TestRun_RepeatedSignatureWithMutationDoesNotTrip(t *testing.T) {
	client := &repeatSeqClient{responses: []llm.ChatResponse{
		stepOf(writeCall("1", "a.txt")),
		stepOf(writeCall("2", "a.txt")),
		stepOf(writeCall("3", "a.txt")),
		stepOf(writeCall("4", "a.txt")),
		say("done"),
	}}
	a := New(Config{Client: client, Tools: NewRegistry(mutStub{"write_file"}), System: "sys", MaxSteps: 10})

	answer, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "done" {
		t.Fatalf("answer = %q, want the final answer", answer)
	}
	if len(a.Report().Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none: mutating repeats are progress", a.Report().Warnings)
	}
}

// Distinct calls are progress even when they are slow, so they never trip.
func TestRun_DistinctSignaturesDoNotTrip(t *testing.T) {
	var runs int32
	client := &repeatSeqClient{responses: []llm.ChatResponse{
		stepOf(readCall("1", "a.txt")),
		stepOf(readCall("2", "b.txt")),
		stepOf(readCall("3", "c.txt")),
		stepOf(readCall("4", "d.txt")),
		say("done"),
	}}
	a := New(Config{Client: client, Tools: NewRegistry(repeatingReadTool{runs: &runs}), System: "sys", MaxSteps: 10})

	answer, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "done" || runs != 4 {
		t.Fatalf("answer = %q runs = %d, want done after four distinct reads", answer, runs)
	}
	if len(a.Report().Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none", a.Report().Warnings)
	}
}

// If even the tool-free turn produces nothing, the run reports why instead of
// pretending it finished.
func TestRun_RepeatedStepsWithNoAnswerFail(t *testing.T) {
	var runs int32
	client := &repeatSeqClient{responses: []llm.ChatResponse{
		stepOf(readCall("1", "a.txt")),
		stepOf(readCall("2", "a.txt")),
		stepOf(readCall("3", "a.txt")),
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "  "}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(repeatingReadTool{runs: &runs}), System: "sys"})

	_, err := a.Run(context.Background(), "task")
	if !errors.Is(err, ErrRepeatingSteps) {
		t.Fatalf("err = %v, want ErrRepeatingSteps", err)
	}
	if errors.Is(err, ErrMaxSteps) {
		t.Fatal("a repeated-signature stop must not be reported as a step-budget stop")
	}
}
