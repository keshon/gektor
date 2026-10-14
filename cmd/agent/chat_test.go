package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/llm"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChatTransportAndReset(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body struct {
			Tools    []any                            `json:"tools"`
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Tools) != 0 || len(body.Messages) != 2 || body.Messages[0].Content != "Stay conversational" {
			t.Errorf("unexpected request: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	client, err := llm.ClientFor("llama", server.URL, "local")
	if err != nil {
		t.Fatal(err)
	}
	client.NoGrammar = true
	var out bytes.Buffer
	err = runChat(context.Background(), client, chat.Config{System: "Stay conversational", MaxTokens: 100, ContextLimit: 1000}, "", strings.NewReader("Hi\n/reset\nHello again\n/quit\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || !strings.Contains(out.String(), "Conversation reset.") {
		t.Fatalf("calls=%d output=%s", count, out.String())
	}
}

func TestChatCancellationWhileInputIdle(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	client, err := llm.ClientFor("llama", "http://127.0.0.1:1", "local")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		done <- runChat(ctx, client, chat.Config{MaxTokens: 100, ContextLimit: 1000}, "", reader, io.Discard)
	}()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("idle input blocked cancellation")
	}
}
