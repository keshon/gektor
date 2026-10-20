package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

// failStreamClient fails Stream with the given error, then serves Chat
// normally: the shape of a backend that chokes on one generation.
type failStreamClient struct {
	streamErr   error
	streamCalls int
	chatCalls   int
}

func (c *failStreamClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	c.chatCalls++
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "recovered"}}, nil
}

func (c *failStreamClient) Stream(_ context.Context, _ llm.ChatRequest, _, onContent func(string)) (llm.ChatResponse, error) {
	c.streamCalls++
	return llm.ChatResponse{}, c.streamErr
}

// A retryable HTTP error on the stream path must fall back to unary
// retry: the status arrives before any SSE data, so nothing streamed
// and there is nothing to resume or duplicate.
func TestAgent_StreamRetryableErrorFallsBackToUnary(t *testing.T) {
	client := &failStreamClient{streamErr: &llm.APIError{
		Status: 500, StatusText: "Internal Server Error", Body: "parse error",
	}}
	a := New(Config{
		Client: client, Tools: NewRegistry(), System: "sys",
		Stream: true})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "recovered" {
		t.Fatalf("result = %q, want the unary fallback answer", out)
	}
	if client.streamCalls != 1 || client.chatCalls != 1 {
		t.Fatalf("stream=%d chat=%d, want exactly one attempt each", client.streamCalls, client.chatCalls)
	}
}

// A non-retryable stream error must still surface immediately without
// touching the unary path.
func TestAgent_StreamNonRetryableErrorReturns(t *testing.T) {
	client := &failStreamClient{streamErr: &llm.APIError{
		Status: 400, StatusText: "Bad Request", Body: "bad request",
	}}
	a := New(Config{
		Client: client, Tools: NewRegistry(), System: "sys",
		Stream: true})

	_, err := a.Run(context.Background(), "task")
	if err == nil {
		t.Fatal("expected the 400 to surface, got nil error")
	}
	if client.chatCalls != 0 {
		t.Fatalf("chatCalls = %d, unary must not run after a non-retryable stream error", client.chatCalls)
	}
}

// thinkThenFailClient streams deliberation, then dies the way a dropped
// connection does: mid-flight, before any answer text.
type thinkThenFailClient struct {
	streamCalls int
	chatCalls   int
}

func (c *thinkThenFailClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	c.chatCalls++
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "recovered"}}, nil
}

func (c *thinkThenFailClient) Stream(_ context.Context, _ llm.ChatRequest, onReasoning, _ func(string)) (llm.ChatResponse, error) {
	c.streamCalls++
	onReasoning("thinking out loud")
	return llm.ChatResponse{}, llm.ErrIncompleteStream
}

// A stream that shows deliberation and then fails must NOT be retried.
// Reasoning reaches the screen like content does, so a retry would print
// the same THINK twice.
func TestAgent_StreamShownReasoningBlocksRetry(t *testing.T) {
	client := &thinkThenFailClient{}
	a := New(Config{
		Client: client, Tools: NewRegistry(), System: "sys",
		Stream: true,
	})
	var shown []string
	a.cfg.OnReasoning = func(chunk string) { shown = append(shown, chunk) }

	if _, err := a.Run(context.Background(), "task"); err == nil {
		t.Fatal("expected the incomplete stream to surface")
	}
	if client.chatCalls != 0 {
		t.Fatalf("chatCalls = %d, a stream that already showed reasoning must not retry", client.chatCalls)
	}
	if len(shown) == 0 {
		t.Fatal("the test proves nothing if reasoning never reached the observer")
	}
}

// The pre-existing "streaming unsupported" fallback keeps working.
func TestAgent_StreamUnsupportedFallsBackToUnary(t *testing.T) {
	client := &failStreamClient{streamErr: fmt.Errorf("%w on koboldcpp", llm.ErrStreamUnsupported)}
	a := New(Config{
		Client: client, Tools: NewRegistry(), System: "sys",
		Stream: true})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "recovered" {
		t.Fatalf("result = %q, want the unary fallback answer", out)
	}
}
