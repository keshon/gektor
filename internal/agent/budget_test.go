package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/llm/chatcompletions"
)

type budgetClient struct {
	calls      int
	closeFails bool
	closing    llm.ChatRequest
}

func (c *budgetClient) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.calls++
	if c.calls == 1 {
		<-ctx.Done()
		return llm.ChatResponse{}, ctx.Err()
	}
	c.closing = req
	if c.closeFails {
		<-ctx.Done()
		return llm.ChatResponse{}, ctx.Err()
	}
	return say("The requested browser check remains unverified."), nil
}

func TestRunBudgetClosesWithoutToolsAndRemainsIncomplete(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, fail := range []bool{false, true} {
			client := &budgetClient{closeFails: fail}
			stateFile := filepath.Join(t.TempDir(), "state.json")
			a := New(Config{
				Client:     client,
				Tools:      NewRegistry(),
				System:     "Preserve the operator's workspace.",
				StateFile:  stateFile,
				MaxSteps:   4,
				RunTimeout: time.Second,
			})
			answer, err := a.Run(t.Context(), "inspect")
			if !errors.Is(err, ErrRunBudget) || answer != "" || a.Report().Final != "" || a.Report().Outcome != "incomplete" {
				t.Fatalf("%q %v %+v", answer, err, a.Report())
			}
			if client.calls != 2 || len(client.closing.Tools) != 0 || a.Report().Summary == "" {
				t.Fatalf("missing bounded closing: %+v", a.Report())
			}
			if !fail && !strings.Contains(a.Report().Summary, "unverified") {
				t.Fatal(a.Report().Summary)
			}

			closing := client.closing.Messages
			if len(closing) < 3 || closing[0].Role != llm.RoleSystem || closing[0].Content != a.cfg.System || closing[1].Content != "inspect" {
				t.Fatalf("budget closing lost the system instructions or task: %+v", closing)
			}
			saved, err := conversation.Load(stateFile)
			if err != nil {
				t.Fatal(err)
			}
			if len(saved.History) < 3 || saved.History[0].Role != llm.RoleSystem || saved.History[0].Content != a.cfg.System || saved.History[1].Content != "inspect" {
				t.Fatalf("budget closing corrupted the saved conversation: %+v", saved.History)
			}
		}
	})
}

func TestRunBudgetNeverAddsARequestPastStepLimit(t *testing.T) {
	client := &budgetClient{}
	a := New(Config{Client: client, Tools: NewRegistry(), MaxSteps: 1, RunTimeout: time.Second})
	_, err := a.Run(t.Context(), "inspect")
	if !errors.Is(err, ErrRunBudget) || client.calls != 1 || a.Report().Summary == "" {
		t.Fatal(client.calls, err, a.Report())
	}
}

// Opt-in transport check; normal tests never contact a running model.
func TestRunBudgetLive(t *testing.T) {
	endpoint := os.Getenv("GEKTOR_BUDGET_LIVE_URL")
	if endpoint == "" {
		t.Skip("set GEKTOR_BUDGET_LIVE_URL to a local llama.cpp endpoint")
	}
	a := New(Config{Client: chatcompletions.NewLlamaClient(endpoint, "local"), Tools: NewRegistry(), System: "You are a coding assistant.", MaxSteps: 4, MaxTokens: 8192, ContextLimit: 32768, RunTimeout: 4 * time.Second, Stream: true})
	started := time.Now()
	_, err := a.Run(t.Context(), "Write a complete HTTP server in Go, followed by extensive tests and a detailed explanation. Produce at least 2000 words.")
	if !errors.Is(err, ErrRunBudget) || a.Report().Summary == "" || a.Report().Outcome != "incomplete" {
		t.Fatalf("err=%v report=%+v", err, a.Report())
	}
	if elapsed := time.Since(started); elapsed > 8*time.Second {
		t.Fatalf("deadline took %v", elapsed)
	}
	t.Logf("elapsed=%v outcome=%s summary=%s", time.Since(started), a.Report().Outcome, a.Report().Summary)
}

func TestUserCancellationNeverRequestsClosing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := &budgetClient{}
	a := New(Config{Client: client, Tools: NewRegistry(), RunTimeout: time.Second})
	if _, err := a.Run(ctx, "inspect"); !errors.Is(err, context.Canceled) || client.calls != 0 {
		t.Fatalf("calls=%d err=%v", client.calls, err)
	}
}

func TestRepeatedObservationsDiagnoseAlternatingReadsWithoutStopping(t *testing.T) {
	a := New(Config{Tools: NewRegistry()})
	var st runState
	for i := range 8 {
		a.noteObservation(&st, "read_file", json.RawMessage(`{"path":"`+string(rune('a'+i%2))+`"}`), "same observation")
	}
	if len(a.Report().Warnings) != 1 {
		t.Fatal(a.Report())
	}
	st.noteMutation()
	if len(st.observations) != 0 {
		t.Fatal("mutation retained old observations")
	}
}
