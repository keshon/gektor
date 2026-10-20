package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
)

type inputClient func(context.Context, llm.ChatRequest) (llm.ChatResponse, error)

func (f inputClient) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	return f(ctx, req)
}

func TestInputWaitsForCompleteToolGroupAndKeepsAttachments(t *testing.T) {
	inbox := &Inbox{}
	calls, accepted := 0, 0
	state := filepath.Join(t.TempDir(), "state.json")
	client := inputClient(func(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		calls++
		if calls == 1 {
			images := []string{"image.png"}
			if err := inbox.Send("steering", images...); err != nil {
				t.Fatal(err)
			}
			images[0] = "mutated"
			if err := inbox.Send("second message"); err != nil {
				t.Fatal(err)
			}
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call", Name: "echo", Arguments: json.RawMessage(`{}`)}}}}, nil
		}
		history := req.Messages
		if len(history) != 6 || history[2].Role != llm.RoleAssistant || history[3].Role != llm.RoleTool || history[4].Content != "steering" || history[5].Content != "second message" {
			t.Fatalf("interleaved or missing inputs: %+v", history)
		}
		if history[4].Images[0] != "image.png" {
			t.Fatal("attachment storage was shared with sender")
		}
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
	})
	a := New(Config{Client: client, Tools: NewRegistry(echoToolStub{}), Inbox: inbox, StateFile: state, OnInput: func(llm.Message) { accepted++ }})
	if answer, err := a.Run(t.Context(), "task"); err != nil || answer != "done" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
	if accepted != 2 || calls != 2 || inbox.Len() != 0 {
		t.Fatalf("accepted=%d calls=%d pending=%d", accepted, calls, inbox.Len())
	}
	if err := inbox.Send("late"); !errors.Is(err, ErrInputClosed) {
		t.Fatalf("late input=%v", err)
	}
	history, err := session.ReadHistory(state)
	if err != nil || history[len(history)-2].Content != "second message" {
		t.Fatalf("saved input missing: %+v %v", history, err)
	}
}

func TestInputArrivingDuringFinalAnswerContinuesSameRun(t *testing.T) {
	inbox := &Inbox{}
	calls := 0
	client := inputClient(func(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		calls++
		if calls == 1 {
			if err := inbox.Send("update the task"); err != nil {
				t.Fatal(err)
			}
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "old answer"}}, nil
		}
		if req.Messages[len(req.Messages)-1].Content != "update the task" {
			t.Fatal("steering missing")
		}
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "updated answer"}}, nil
	})
	a := New(Config{Client: client, Tools: NewRegistry(), Inbox: inbox})
	if answer, err := a.Run(t.Context(), "task"); err != nil || answer != "updated answer" || a.Report().Steps != 2 {
		t.Fatalf("answer=%q err=%v report=%+v", answer, err, a.Report())
	}
}

func TestInputIsSavedWhenCurrentRequestIsCancelled(t *testing.T) {
	inbox := &Inbox{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	state := filepath.Join(t.TempDir(), "state.json")
	client := inputClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		if err := inbox.Send("do not lose this"); err != nil {
			t.Fatal(err)
		}
		cancel()
		return llm.ChatResponse{}, ctx.Err()
	})
	a := New(Config{Client: client, Tools: NewRegistry(), Inbox: inbox, StateFile: state})
	if _, err := a.Run(ctx, "task"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	history, err := session.ReadHistory(state)
	if err != nil || history[len(history)-1].Content != "do not lose this" {
		t.Fatalf("lost input: %+v %v", history, err)
	}
}

func TestInboxFinishAndConcurrentSendCannotLoseMessage(t *testing.T) {
	for range 500 {
		inbox := &Inbox{}
		done := make(chan error, 1)
		go func() { done <- inbox.Send("message") }()
		inputs := inbox.take(true)
		err := <-done
		inputs = append(inputs, inbox.stop()...)
		if (err == nil && len(inputs) != 1) || (err != nil && (!errors.Is(err, ErrInputClosed) || len(inputs) != 0)) {
			t.Fatalf("send=%v messages=%v", err, inputs)
		}
	}
}

func TestModelSeesRemainingStepBudgetWithoutChangingSystemPrefix(t *testing.T) {
	calls := 0
	client := inputClient(func(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		calls++
		if req.Messages[0].Content != "fixed system" {
			t.Fatal("budget changed system prefix")
		}
		if calls == 1 {
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call", Name: "echo", Arguments: json.RawMessage(`{}`)}}}}, nil
		}
		if req.Messages[len(req.Messages)-1].Content != harnessText("2 model steps remain in this run, including the final answer. Finish the task or report concrete progress and remaining work.") {
			t.Fatalf("no remaining budget: %+v", req.Messages)
		}
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
	})
	a := New(Config{Client: client, Tools: NewRegistry(echoToolStub{}), System: "fixed system", MaxSteps: 3})
	if _, err := a.Run(t.Context(), "task"); err != nil {
		t.Fatal(err)
	}
}

func TestSteeringDuringRepeatRecoveryAnswerContinuesRun(t *testing.T) {
	inbox := &Inbox{}
	calls := 0
	sent := false
	client := inputClient(func(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		calls++
		if sent {
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "updated"}}, nil
		}
		if len(req.Tools) == 0 {
			if err := inbox.Send("new direction"); err != nil {
				t.Fatal(err)
			}
			sent = true
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "recovery answer"}}, nil
		}
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call", Name: "echo", Arguments: json.RawMessage(`{}`)}}}}, nil
	})
	a := New(Config{Client: client, Tools: NewRegistry(echoToolStub{}), Inbox: inbox, MaxSteps: 10})
	if answer, err := a.Run(t.Context(), "task"); err != nil || answer != "updated" || a.Report().Steps != calls {
		t.Fatalf("answer=%q err=%v report=%+v calls=%d", answer, err, a.Report(), calls)
	}
}
