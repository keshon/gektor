package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/session"
)

type steeringTool struct {
	name string
	mode ToolMode
	run  func(context.Context) (string, error)
}

func (s steeringTool) Name() string                                               { return s.name }
func (s steeringTool) Description() string                                        { return s.name }
func (s steeringTool) Schema() json.RawMessage                                    { return json.RawMessage(`{"type":"object"}`) }
func (s steeringTool) Mode() ToolMode                                             { return s.mode }
func (s steeringTool) Run(ctx context.Context, _ json.RawMessage) (string, error) { return s.run(ctx) }

func steeringCalls(names ...string) []llm.ToolCall {
	var calls []llm.ToolCall
	for _, name := range names {
		calls = append(calls, llm.ToolCall{ID: name, Name: name, Arguments: json.RawMessage(`{}`)})
	}
	return calls
}

func TestSteeringStopsUnstartedToolsButFinishesActiveTool(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, mode := range []ToolMode{Concurrent, Exclusive} {
			inbox := &Inbox{}
			inbox.SetSteering(enabled)
			var mutations atomic.Int32
			first := steeringTool{"first", mode, func(ctx context.Context) (string, error) {
				if err := inbox.Send("change direction", "image.png"); err != nil {
					t.Fatal(err)
				}
				if ctx.Err() != nil {
					t.Fatal("steering cancelled active tool")
				}
				return "finished", nil
			}}
			write := steeringTool{"write", Exclusive, func(context.Context) (string, error) { mutations.Add(1); return "written", nil }}
			a := New(Config{Inbox: inbox, Tools: NewRegistry(first, write), MutatingTools: []string{"write"}})
			st := &runState{}
			history := a.executeToolCalls(t.Context(), st, 0, "stop", steeringCalls("first", "write"), nil)
			if len(history) != 2 || history[0].Content != "finished" {
				t.Fatal(history)
			}
			if enabled {
				if mutations.Load() != 0 || st.mutatingSucceeded != 0 || history[1].ToolStatus != "skipped_for_input" || *history[1].ToolFailed {
					t.Fatal(history, st)
				}
				if _, ok := inbox.RecallLatest(); ok {
					t.Fatal("claimed input was recalled")
				}
				pending := inbox.stop()
				if len(pending) != 1 || pending[0].Images[0] != "image.png" {
					t.Fatal("lost claimed input", pending)
				}
			} else if mutations.Load() != 1 {
				t.Fatal("disabled steering withheld work")
			}
		}
	}
}

func TestSteeringRechecksAfterPermissionAndLatches(t *testing.T) {
	inbox := &Inbox{}
	inbox.SetSteering(true)
	ran := false
	tool := steeringTool{"write", Exclusive, func(context.Context) (string, error) { ran = true; return "", nil }}
	a := New(Config{Inbox: inbox, Tools: NewRegistry(tool), Policy: permission.Policy{Rules: []permission.Rule{{Tool: "*", Pattern: "*", Effect: permission.Ask}}},
		BeforeToolCall: func(context.Context, string, string, json.RawMessage) (permission.Effect, error) {
			_ = inbox.Send("do not write")
			return permission.Allow, nil
		}, OnResult: func(ToolResult) { inbox.SetSteering(false) }})
	history := a.executeToolCalls(t.Context(), &runState{}, 0, "stop", steeringCalls("write", "write"), nil)
	if ran || len(history) != 2 || history[0].ToolStatus != "skipped_for_input" || history[1].ToolStatus != "skipped_for_input" {
		t.Fatal(history)
	}
	yielded := true
	inbox.SetSteering(false)
	if inbox.admit(&yielded) {
		t.Fatal("turning off steering resumed a yielded group")
	}
}

func TestSteeringRecallRacesAdmission(t *testing.T) {
	for range 200 {
		inbox := &Inbox{}
		inbox.SetSteering(true)
		_ = inbox.Send("redirect")
		admitted := make(chan bool, 1)
		go func() { yielded := false; admitted <- inbox.admit(&yielded) }()
		_, recalled := inbox.RecallLatest()
		allowed := <-admitted
		pending := inbox.stop()
		if recalled != allowed || (recalled && len(pending) != 0) || (!recalled && len(pending) != 1) {
			t.Fatal("lost or recalled an accepted instruction", recalled, allowed, pending)
		}
	}
}

func TestSteeringBeforeDispatchKeepsCompleteHistory(t *testing.T) {
	inbox := &Inbox{}
	inbox.SetSteering(true)
	var requests int
	ran := false
	client := inputClient(func(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		requests++
		if requests == 1 {
			_ = inbox.Send("instead explain", "image.png")
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: steeringCalls("write")}}, nil
		}
		h := req.Messages
		n := len(h)
		if n < 3 || h[n-3].Role != llm.RoleAssistant || h[n-2].Role != llm.RoleTool || h[n-2].ToolStatus != "skipped_for_input" || h[n-1].Content != "instead explain" || h[n-1].Images[0] != "image.png" {
			t.Fatal(h)
		}
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
	})
	tool := steeringTool{"write", Exclusive, func(context.Context) (string, error) { ran = true; return "", nil }}
	a := New(Config{Client: client, Inbox: inbox, Tools: NewRegistry(tool)})
	if _, err := a.Run(t.Context(), "task"); err != nil {
		t.Fatal(err)
	}
	if ran || requests != 2 {
		t.Fatal(ran, requests)
	}
}

func TestToolCompletionPreviewDoesNotWaitForSibling(t *testing.T) {
	release := make(chan struct{})
	fast := steeringTool{"fast", Concurrent, func(context.Context) (string, error) { return "fast", nil }}
	slow := steeringTool{"slow", Concurrent, func(context.Context) (string, error) { <-release; return "slow", nil }}
	finished := []string{}
	a := New(Config{Tools: NewRegistry(fast, slow), OnToolFinished: func(id string) {
		finished = append(finished, id)
		if id == "fast" {
			close(release)
		}
	}})
	history := a.executeToolCalls(t.Context(), &runState{}, 0, "stop", steeringCalls("slow", "fast"), nil)
	if len(finished) != 2 || finished[0] != "fast" || history[0].ToolCallID != "slow" || history[1].ToolCallID != "fast" {
		t.Fatal(finished, history)
	}
}

func TestSteeringAcceptedInputSurvivesCancellation(t *testing.T) {
	inbox := &Inbox{}
	inbox.SetSteering(true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	path := filepath.Join(t.TempDir(), "state.json")
	client := inputClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		_ = inbox.Send("redirect after retry", "image.png")
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: steeringCalls("echo")}}, nil
	})
	a := New(Config{Client: client, Inbox: inbox, Tools: NewRegistry(echoToolStub{}), StateFile: path,
		OnResult: func(r ToolResult) {
			if r.Status != "skipped_for_input" {
				t.Fatal(r)
			}
			cancel()
		}})
	if _, err := a.Run(ctx, "task"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	history, err := session.ReadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	last := history[len(history)-1]
	if last.Content != "redirect after retry" || len(last.Images) != 1 || last.Images[0] != "image.png" {
		t.Fatal(history)
	}
}
