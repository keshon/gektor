package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
)

func TestCompletionRetainsArrivingNotificationsAndInput(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		for _, forced := range []bool{false, true} {
			for _, limit := range []bool{false, true} {
				for _, event := range []string{"notification", "notification_with_inbox", "input", "both"} {
					t.Run(fmt.Sprintf("persisted=%v/forced=%v/limit=%v/%s", persisted, forced, limit, event), func(t *testing.T) {
						var inbox *Inbox
						if event != "notification" {
							inbox = &Inbox{}
						}
						wantInput := event == "input" || event == "both"
						wantNote := event != "input"
						finishCall := 1
						if forced {
							finishCall = 4
						}
						maxSteps := 8
						if limit {
							maxSteps = finishCall
						}
						calls, delivered := 0, 0
						pending := false
						client := inputClient(func(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
							calls++
							if calls < finishCall {
								return stepOf(llm.ToolCall{ID: fmt.Sprint(calls), Name: "echo", Arguments: json.RawMessage(`{}`)}), nil
							}
							if calls == finishCall {
								pending = wantNote
								if wantInput {
									if err := inbox.Send("new direction"); err != nil {
										t.Fatal(err)
									}
								}
								return say("early answer"), nil
							}
							if wantNote && !historyContains(req.Messages, "background finished") {
								t.Fatal("notification missing from next request")
							}
							if wantInput && !historyContains(req.Messages, "new direction") {
								t.Fatal("input missing from next request")
							}
							if forced {
								toolResults := 0
								for _, message := range req.Messages {
									if message.Role == llm.RoleTool {
										toolResults++
									}
								}
								if toolResults != 3 || !historyContains(req.Messages, "early answer") {
									t.Fatal("forced continuation lost history", req.Messages)
								}
							}
							return say("updated answer"), nil
						})
						path := ""
						if persisted {
							path = filepath.Join(t.TempDir(), "state.json")
						}
						a := New(Config{Client: client, Tools: NewRegistry(echoToolStub{}), Inbox: inbox, MaxSteps: maxSteps, StateFile: path,
							Notifications: func(context.Context) []string {
								if !pending {
									return nil
								}
								pending = false
								delivered++
								return []string{"background finished"}
							},
						})
						answer, err := a.Run(t.Context(), "task")
						if limit {
							if !errors.Is(err, ErrMaxSteps) || answer != "" || a.Report().Final != "" || calls != finishCall {
								t.Fatalf("limit: answer=%q err=%v calls=%d report=%+v", answer, err, calls, a.Report())
							}
						} else if err != nil || answer != "updated answer" || calls != finishCall+1 {
							t.Fatalf("answer=%q err=%v calls=%d", answer, err, calls)
						}
						if a.Report().Steps != calls || pending || (wantNote && delivered != 1) {
							t.Fatalf("calls=%d delivered=%d pending=%v report=%+v", calls, delivered, pending, a.Report())
						}
						history := a.snapshot.History
						if persisted {
							var err error
							history, err = conversation.ReadHistory(path)
							if err != nil {
								t.Fatal(err)
							}
						}
						if wantNote && !historyContains(history, "background finished") {
							t.Fatal("notification not saved")
						}
						if wantInput && !historyContains(history, "new direction") {
							t.Fatal("input not saved")
						}
						if inbox != nil && !errors.Is(inbox.Send("late"), ErrInputClosed) {
							t.Fatal("completed run still accepts input")
						}
					})
				}
			}
		}
	}
}

func historyContains(history []llm.Message, text string) bool {
	for _, message := range history {
		if strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}

func TestAgent_EmptyFinish_RetriesOnceBeforeReturning(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: ""}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "actual answer"}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys"})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("calls = %d, want 2 (empty finish retried once)", client.calls)
	}
	if out != "actual answer" {
		t.Fatalf("result = %q, want %q", out, "actual answer")
	}
}

func TestAgent_EmptyFinish_GivesUpAfterRetry(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: ""}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: ""}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys"})

	_, err := a.Run(context.Background(), "task")
	if err == nil {
		t.Fatalf("Run err = %v, want wrapped max-steps", err)
	}
	if client.calls != 2 {
		t.Fatalf("calls = %d, want 2 (one retry, then give up)", client.calls)
	}
}

func TestAgent_ModeSwitchingFailuresStillTerminate(t *testing.T) {
	// Alternates leaked-text finishes and empty finishes: neither mode
	// alone may strand the loop, and switching must not reset the
	// accounting. Scripted stubs bound execution by construction (the
	// stub panics past its script), so termination here is proven, not
	// merely bounded by MaxSteps.
	leak := llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant,
		Content: "Here's the file.\n<|tool_call>call:write_file{path: \"x.txt\"}<tool_call|>"}}
	empty := llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: ""}}
	client := &stubClient{responses: []llm.ChatResponse{leak, empty, leak, empty}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys", MaxSteps: 12})

	_, err := a.Run(context.Background(), "task")
	if err == nil {
		t.Fatalf("Run err = %v, want wrapped max-steps", err)
	}
	if client.calls != 4 {
		t.Fatalf("calls = %d, want 4 (two leaks escalate, second empty gives up)", client.calls)
	}
}

func TestAgent_SkipVerify_ReturnsImmediately(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys"})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("calls = %d, want 1", client.calls)
	}
	if out != "done" {
		t.Fatalf("result = %q, want %q", out, "done")
	}
}

func TestExplanationDoesNotRequireFileEffects(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{say("The function was removed from main.go.")}}
	a := New(Config{Client: client, Tools: NewRegistry()})
	out, err := a.Run(context.Background(), "Explain the change")
	if err != nil || out != "The function was removed from main.go." || client.calls != 1 {
		t.Fatalf("out=%q err=%v calls=%d", out, err, client.calls)
	}
}

func TestAgent_PlainAnswerSkipsVerify(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "hey there!"}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys"})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// No tools, no file claims: one call, answer straight through,
	// no interrogation appended.
	if client.calls != 1 {
		t.Fatalf("calls = %d, want 1", client.calls)
	}
	if out != "hey there!" {
		t.Fatalf("result = %q", out)
	}
	for _, m := range client.lastHistory {
		if m.Role == llm.RoleUser && strings.HasPrefix(m.Content, "[harness] ") {
			t.Fatalf("chit-chat must not summon a nudge: %q", m.Content)
		}
	}
}

func TestAgent_LeakedToolCallText_IsNotTrustedAsFinish(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant,
			Content: "Here's the file.\n<|tool_call>call:write_file{path: \"x.txt\"}<tool_call|>"}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done for real"}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys"})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "done for real" {
		t.Fatalf("result = %q, want %q", out, "done for real")
	}

	found := false
	for _, m := range client.lastHistory {
		if m.Role == llm.RoleUser && m.Content == harnessText(prompts.LeakDetected) {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a corrective nudge about the leaked tool-call text")
	}
}

func TestAgent_GemmaStyleLeakedToolCall_IsNotTrustedAsFinish(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `(Made a function call call_92023 to read_file with arguments={"path": "sample.go"})`}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done for real"}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys"})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "done for real" {
		t.Fatalf("result = %q, want %q", out, "done for real")
	}
	for _, m := range client.lastHistory {
		if m.Role == llm.RoleUser && m.Content == harnessText(prompts.LeakDetected) {
			return
		}
	}
	t.Fatal("expected leak nudge for Gemma-style pseudo tool-call text")
}

// A leaked call naming a real tool with parseable args executes instead
// of costing a nudge round-trip: recovery, not refusal.
func TestAgent_LeakedCallRecoversAndExecutes(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `(Made a function call call_1 to echo with arguments={})`}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(echoToolStub{}), System: "sys"})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "done" {
		t.Fatalf("result = %q, want done", out)
	}
	for _, m := range client.lastHistory {
		if m.Role == llm.RoleTool && m.Content == "ok" {
			return
		}
	}
	t.Fatal("expected the recovered call to execute and record a tool result")
}

func TestAgent_VerifyHook_SkippedWhenNotOK(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "confirmed"}},
	}}
	a := New(Config{
		Client: client,
		Tools:  NewRegistry(),
		System: "sys",
		Verify: func(ctx context.Context) (string, error) { return "", nil },
	})

	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, m := range client.lastHistory {
		if strings.Contains(m.Content, "Build/test check") {
			t.Fatalf("unexpected verify output when Verify reported ok=false: %q", m.Content)
		}
	}
}
