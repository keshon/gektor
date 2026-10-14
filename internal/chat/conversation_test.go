package chat

import (
	"context"
	"errors"
	"github.com/keshon/tars/internal/llm"
	"strings"
	"testing"
)

type clientFunc func(context.Context, llm.ChatRequest) (llm.ChatResponse, error)

func (f clientFunc) Chat(ctx context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	return f(ctx, r)
}
func TestFailedTurnsAndToolRequestsNeverEnterHistory(t *testing.T) {
	for _, mode := range []string{"failure", "canceled", "tools", "length", "empty"} {
		t.Run(mode, func(t *testing.T) {
			count := 0
			client := clientFunc(func(ctx context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
				count++
				if len(r.Tools) != 0 {
					t.Fatal("chat offered tools")
				}
				if count == 1 {
					response := llm.ChatResponse{Message: llm.Message{Content: "bad"}}
					switch mode {
					case "failure":
						return response, errors.New("offline")
					case "canceled":
						return response, context.Canceled
					case "tools":
						response.Message.ToolCalls = []llm.ToolCall{{Name: "run_shell"}}
					case "length":
						response.FinishReason = "length"
					case "empty":
						response.Message.Content = ""
					}
					return response, nil
				}
				if len(r.Messages) != 2 || r.Messages[1].Content != "retry" {
					t.Fatalf("failed turn leaked: %+v", r.Messages)
				}
				return llm.ChatResponse{Message: llm.Message{Content: "ok"}}, nil
			})
			c, err := New(client, Config{System: "system", MaxTokens: 100, ContextLimit: 1000})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = c.Ask(context.Background(), "failed"); err == nil {
				t.Fatal("expected failure")
			}
			if _, err = c.Ask(context.Background(), "retry"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestWholeTurnWindowAndReset(t *testing.T) {
	calls := 0
	client := clientFunc(func(_ context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
		calls++
		if calls == 3 {
			if len(r.Messages) != 4 || r.Messages[1].Content != "second" || r.Messages[2].Role != llm.RoleAssistant {
				t.Fatalf("window split or reordered pair: %+v", r.Messages)
			}
		}
		if calls == 4 && len(r.Messages) != 2 {
			t.Fatal("reset retained history")
		}
		return llm.ChatResponse{Message: llm.Message{Content: strings.Repeat("a", 70)}}, nil
	})
	c, err := New(client, Config{System: "system", MaxTokens: 100, ContextLimit: 500})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"first", "second", "third"} {
		r, err := c.Ask(context.Background(), text)
		if err != nil {
			t.Fatal(err)
		}
		if text == "third" && r.OmittedTurns != 1 {
			t.Fatalf("omissions=%d", r.OmittedTurns)
		}
	}
	if len(c.history) != 6 {
		t.Fatal("projection destroyed completed history")
	}
	c.Reset()
	if _, err := c.Ask(context.Background(), "fresh"); err != nil {
		t.Fatal(err)
	}
}
func TestOversizedInputDoesNotCallBackend(t *testing.T) {
	c, err := New(clientFunc(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		t.Fatal("oversized request reached backend")
		return llm.ChatResponse{}, nil
	}), Config{MaxTokens: 100, ContextLimit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ask(context.Background(), strings.Repeat("x", 1000)); err == nil {
		t.Fatal("expected budget error")
	}
}

func TestGreetingAndPostHistoryProjection(t *testing.T) {
	c, err := New(clientFunc(func(_ context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
		if len(r.Messages) != 4 || r.Messages[0].Role != llm.RoleSystem || r.Messages[1].Role != llm.RoleAssistant || r.Messages[1].Content != "Hello Alex" || r.Messages[2].Role != llm.RoleUser || r.Messages[3].Role != llm.RoleSystem || r.Messages[3].Content != "Latest directive" || r.Temperature != .7 || len(r.Tools) > 0 {
			t.Fatalf("bad projection: %+v", r)
		}
		return llm.ChatResponse{Message: llm.Message{Content: "reply"}}, nil
	}), Config{System: "persona", Greeting: "Hello Alex", PostHistory: "Latest directive", Temperature: .7, MaxTokens: 100, ContextLimit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Ask(context.Background(), "Hi"); err != nil {
		t.Fatal(err)
	}
	if len(c.History()) != 2 {
		t.Fatal("greeting or directive leaked into completed-turn history")
	}
}
func TestPinnedPromptComponentsCountTowardBudget(t *testing.T) {
	client := clientFunc(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		t.Fatal("oversized prompt reached backend")
		return llm.ChatResponse{}, nil
	})
	for _, cfg := range []Config{{Greeting: strings.Repeat("g", 500), MaxTokens: 100, ContextLimit: 500}, {PostHistory: strings.Repeat("p", 500), MaxTokens: 100, ContextLimit: 500}} {
		c, err := New(client, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = c.Ask(context.Background(), "Hi"); err == nil {
			t.Fatal("pinned prompt ignored in budget")
		}
	}
}

func TestInlineReasoningNeverEntersSavedDialogue(t *testing.T) {
	c, _ := New(clientFunc(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{Message: llm.Message{Content: "<think>private deliberation</think>Hello"}}, nil
	}), Config{MaxTokens: 100, ContextLimit: 1000})
	result, err := c.Ask(context.Background(), "Hi")
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Message.Content != "Hello" || c.History()[1].Content != "Hello" {
		t.Fatal("reasoning leaked into dialogue")
	}
}

type streamingClient struct {
	delta  chan string
	finish chan struct{}
}

func (s *streamingClient) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}
func (s *streamingClient) Stream(ctx context.Context, _ llm.ChatRequest, onDelta func(string)) (llm.ChatResponse, error) {
	onDelta("First sentence. ")
	s.delta <- "sent"
	select {
	case <-s.finish:
	case <-ctx.Done():
		return llm.ChatResponse{}, ctx.Err()
	}
	onDelta("Last sentence.")
	return llm.ChatResponse{Message: llm.Message{Content: "First sentence. Last sentence."}}, nil
}
func TestStreamedTurnCommitsOnlyAfterCompletion(t *testing.T) {
	client := &streamingClient{delta: make(chan string, 1), finish: make(chan struct{})}
	c, _ := New(client, Config{MaxTokens: 100, ContextLimit: 1000})
	deltas := make(chan string, 2)
	done := make(chan error, 1)
	go func() {
		_, err := c.AskStreaming(context.Background(), "Hi", func(text string) { deltas <- text })
		done <- err
	}()
	<-client.delta
	if <-deltas != "First sentence. " {
		t.Fatal("first delta unavailable")
	}
	close(client.finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(c.History()) != 2 {
		t.Fatal("stream not committed")
	}
}

func TestRuntimeSpeechNoteIsBudgetedAndNotSaved(t *testing.T) {
	note := "English speech only"
	c, _ := New(clientFunc(func(_ context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
		if r.Messages[0].Role != llm.RoleSystem || !strings.Contains(r.Messages[0].Content, note) {
			t.Fatal("runtime capability missing")
		}
		return llm.ChatResponse{Message: llm.Message{Content: "reply"}}, nil
	}), Config{MaxTokens: 100, ContextLimit: 1000})
	if _, err := c.AskStreaming(context.Background(), "Hi", nil, note); err != nil {
		t.Fatal(err)
	}
	if len(c.History()) != 2 || c.config.System != "" {
		t.Fatal("runtime note polluted durable prompt/history")
	}
	if _, err := c.AskStreaming(context.Background(), "Hi", nil, strings.Repeat("x", 1000)); err == nil {
		t.Fatal("runtime capability note ignored in budget")
	}
}
