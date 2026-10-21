package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/session"
)

// Live failure shape (Qwen3.6 run, 2026-07-11): the backend cut generation
// off after 38 tokens (finish_reason "length"), discarding the tool call
// the model had announced, and the loop accepted "Let me write the
// plan.md file…" as a final answer. A truncated response is never a finish.
func TestAgent_TruncatedFinish_NudgesAndContinues(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{
			Message:      llm.Message{Role: llm.RoleAssistant, Content: "Let me write the file now."},
			FinishReason: "length",
		},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "actual answer"}, FinishReason: "stop"},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys"})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "actual answer" {
		t.Fatalf("result = %q — the truncated stump must not be accepted as the answer", out)
	}
	var nudged bool
	for _, m := range client.lastHistory {
		if m.Role == llm.RoleUser && m.Content == harnessText(prompts.Truncated) {
			nudged = true
		}
	}
	if !nudged {
		t.Fatal("truncation nudge missing from history")
	}
}

func TestAgent_PersistentTruncation_BoundedByMaxSteps(t *testing.T) {
	client := &alwaysTruncatedClient{}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys", MaxSteps: 4})

	_, err := a.Run(context.Background(), "task")
	if err == nil || !errors.Is(err, ErrMaxSteps) {
		t.Fatalf("err = %v, want bounded max-steps failure, not an infinite nudge loop", err)
	}
	if client.calls != 4 {
		t.Fatalf("calls = %d, want exactly MaxSteps calls", client.calls)
	}
}

func TestAgent_ToolCallsWithLengthFinish_ExecuteNormally(t *testing.T) {
	// koboldcpp only returns fully parsed tool calls; length alongside
	// tool_calls means the calls that made it through are complete.
	args := json.RawMessage(`{"path":"a.txt","content":"x"}`)
	client := &stubClient{responses: []llm.ChatResponse{
		{
			Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "1", Name: "write_file", Arguments: args}}},
			FinishReason: "length",
		},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}, FinishReason: "stop"},
	}}
	a := New(Config{
		Client: client, Tools: NewRegistry(mutStub{"write_file"}),
		System: "sys"})

	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if a.LastRunMutations != 1 {
		t.Fatalf("mutations = %d, want 1 — the delivered tool call must execute", a.LastRunMutations)
	}
}

type alwaysTruncatedClient struct{ calls int }

func (c *alwaysTruncatedClient) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	c.calls++
	return llm.ChatResponse{
		Message:      llm.Message{Role: llm.RoleAssistant, Content: "Let me just"},
		FinishReason: "length",
	}, nil
}

// Truncated arguments (generation cut off mid-call) are stumps, not
// calls: executing them fails arg parsing here and 500s backends that
// parse server-side. Refuse with reissue-smaller guidance instead, and
// keep going on the next response.
func TestAgent_TruncatedArgs_RefusedWithGuidance(t *testing.T) {
	var runs int32
	client := &stubClient{responses: []llm.ChatResponse{
		{
			Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "1", Name: "write_file", Arguments: json.RawMessage(`{"path":"a.txt","content":"xxx`)}},
			},
			FinishReason: "length",
		},
		say("done"),
	}}
	a := New(Config{
		Client: client, Tools: NewRegistry(countingStub{name: "write_file", runs: &runs}),
		System: "sys"})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "done" {
		t.Fatalf("result = %q, want the follow-up answer", out)
	}
	if runs != 0 {
		t.Fatalf("truncated call executed %d times, want 0", runs)
	}
	found := false
	for _, m := range client.lastHistory {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "truncated") {
			found = true
		}
	}
	if !found {
		t.Fatal("no truncated-args guidance reached the model")
	}
}

// A mixed batch executes the valid calls and refuses only the stumps.
func TestAgent_TruncatedArgs_MixedBatchExecutesValid(t *testing.T) {
	var runs int32
	writeArgs, _ := json.Marshal(map[string]string{"path": "b.txt", "content": "ok"})
	client := &stubClient{responses: []llm.ChatResponse{
		{
			Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "1", Name: "write_file", Arguments: writeArgs},
				{ID: "2", Name: "write_file", Arguments: json.RawMessage(`{"path":"c.txt","content":"xxx`)}},
			},
			FinishReason: "length",
		},
		say("done"),
	}}
	a := New(Config{
		Client: client, Tools: NewRegistry(countingStub{name: "write_file", runs: &runs}),
		System: "sys"})

	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runs != 1 {
		t.Fatalf("valid call executed %d times, want 1", runs)
	}
	var ok, refused bool
	for _, m := range client.lastHistory {
		if m.Role != llm.RoleTool {
			continue
		}
		if strings.Contains(m.Content, "ok") {
			ok = true
		}
		if strings.Contains(m.Content, "truncated") {
			refused = true
		}
	}
	if !ok || !refused {
		t.Fatalf("want one executed result and one truncation refusal (ok=%v refused=%v)", ok, refused)
	}
}

// The original regression tests only checked execution with a permissive stub.
// Real snapshots and backends must also be able to encode the refused call.
func TestAgent_InvalidArgsRemainPersistableAndReplayable(t *testing.T) {
	for _, finish := range []string{"length", "tool_calls", "stop", ""} {
		for _, args := range []string{`{"path":"a.txt","content":"cut`, "", "not JSON"} {
			t.Run(finish+"/"+args, func(t *testing.T) {
				var runs int32
				bad := json.RawMessage(args)
				client := &stubClient{responses: []llm.ChatResponse{
					{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
						{ID: "bad", Name: "write_file", Arguments: bad},
						{ID: "good", Name: "write_file", Arguments: json.RawMessage(`{"path":"b.txt","content":"ok"}`)},
					}}, FinishReason: finish},
					say("done"),
				}}
				state := filepath.Join(t.TempDir(), "state.json")
				a := New(Config{
					Client: client, Tools: NewRegistry(countingStub{name: "write_file", runs: &runs}),
					System: "sys", StateFile: state,
					OnStep: func(step int, msg llm.Message) {
						if _, err := json.Marshal(msg); err != nil {
							t.Fatalf("display message cannot be encoded: %v", err)
						}
						if step == 1 {
							snapshot, err := session.Load(state)
							if err != nil {
								t.Fatal(err)
							}
							found := false
							for _, m := range snapshot.History {
								if m.ToolCallID == "bad" && m.ToolFailed != nil && *m.ToolFailed {
									found = true
								}
							}
							if !found {
								t.Fatal("refused call was not persisted before the next response")
							}
						}
					},
				})
				if out, err := a.Run(t.Context(), "task"); err != nil || out != "done" {
					t.Fatalf("run: %q %v", out, err)
				}
				if runs != 1 {
					t.Fatalf("executed %d calls; only the valid call may run", runs)
				}
				if _, err := json.Marshal(client.lastHistory); err != nil {
					t.Fatalf("next request cannot be encoded: %v", err)
				}
				if _, err := session.Load(state); err != nil {
					t.Fatal(err)
				}
				if len(a.report.Warnings) != 0 {
					t.Fatalf("unexpected warnings: %v", a.report.Warnings)
				}
				if string(client.responses[0].Message.ToolCalls[0].Arguments) != args {
					t.Fatal("history sanitization mutated the original execution arguments")
				}
			})
		}
	}
}

func TestTruncatedHint_AppendedOnlyForWriters(t *testing.T) {
	run := func(reg *Registry) []llm.Message {
		client := &stubClient{responses: []llm.ChatResponse{
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "partial…"}, FinishReason: "length"},
			say("done"),
		}}
		a := New(Config{
			Client: client, Tools: reg, System: "sys"})
		if _, err := a.Run(context.Background(), "task"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		return client.lastHistory
	}
	if h := run(NewRegistry(echoToolStub{name: "write_file"})); countInjected(h, "smaller pieces") != 1 {
		t.Fatal("writer must get the write-in-pieces advice")
	}
	if h := run(NewRegistry()); countInjected(h, "smaller pieces") != 0 {
		t.Fatal("read-only agent must not get write advice")
	}
}
