package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/llm"
)

type titleClient func(context.Context, llm.ChatRequest) (llm.ChatResponse, error)

func (f titleClient) Chat(ctx context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	return f(ctx, r)
}

func TestGenerateTitleBoundsRequestAndUsesUserLanguage(t *testing.T) {
	calls := 0
	client := titleClient(func(ctx context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 20*time.Second || len(r.Tools) != 0 || r.MaxTokens != 384 || len(r.Messages) != 2 || utf8.RuneCountInString(r.Messages[1].Content) != 2000 || len(r.Messages[1].Images) != 0 {
			t.Fatal("unbounded title request", r)
		}
		return llm.ChatResponse{Message: llm.Message{Content: "“Исправление очереди сообщений”"}}, nil
	})
	got, err := GenerateSessionTitle(t.Context(), client, strings.Repeat("Ж", 3000))
	if err != nil || got != "Исправление очереди сообщений" || calls != 1 {
		t.Fatal(got, err, calls)
	}
}

func TestGenerateTitleRejectsBadResponsesWithoutRetry(t *testing.T) {
	for _, tc := range []struct {
		name, text, finish string
		tool               bool
	}{
		{name: "empty"}, {name: "long", text: strings.Repeat("x", 61)},
		{name: "multiline", text: "Explanation\nActual title"}, {name: "control", text: "Title\x1b[31m"},
		{name: "bidi", text: "Title\u202ereversed"}, {name: "unicode newline", text: "Title\u2028second line"},
		{name: "reasoning", text: "<think>Thinking</think>"}, {name: "truncated", text: "Partial title", finish: "length"},
		{name: "tool", text: "Title", tool: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := titleClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
				calls++
				resp := llm.ChatResponse{Message: llm.Message{Content: tc.text}, FinishReason: tc.finish}
				if tc.tool {
					resp.Message.ToolCalls = []llm.ToolCall{{Name: "write_file"}}
				}
				return resp, nil
			})
			if title, err := GenerateSessionTitle(t.Context(), client, "Fix the queue"); err == nil || title != "" || calls != 1 {
				t.Fatal(title, err, calls)
			}
		})
	}
}

func TestGenerateTitleHonorsCancellationAndBackendFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := titleClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		t.Fatal("called backend after cancellation")
		return llm.ChatResponse{}, nil
	})
	if _, err := GenerateSessionTitle(ctx, client, "task"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	want := errors.New("offline")
	calls := 0
	client = func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		calls++
		return llm.ChatResponse{}, want
	}
	if _, err := GenerateSessionTitle(t.Context(), client, "task"); !errors.Is(err, want) || calls != 1 {
		t.Fatal(err, calls)
	}
}
