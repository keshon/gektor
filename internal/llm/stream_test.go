package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamRequestsMeasuredUsageAndReportsCacheHits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !body.StreamOptions.IncludeUsage {
			t.Error("stream did not request usage")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":5,\"prompt_tokens_details\":{\"cached_tokens\":90}}}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	c := NewLlamaClient(srv.URL, "test")
	resp, err := c.Stream(t.Context(), ChatRequest{Messages: []Message{{Role: RoleUser, Content: "task"}}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Usage.Estimated || resp.Usage.PromptTokens != 100 || resp.Usage.CachedTokens != 90 || c.CachedTotal() != 90 {
		t.Fatalf("measured usage lost: %+v", resp.Usage)
	}
}

func TestPrefillTimingsDoNotReplaceFullPromptSize(t *testing.T) {
	for _, cached := range []int{0, 1024} {
		t.Run(fmt.Sprint(cached), func(t *testing.T) {
			const total = 3398
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, evaluated := range []int{512, 1024, 1536} {
					fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}],\"prompt_progress\":{\"total\":%d,\"cache\":%d,\"processed\":%d},\"timings\":{\"prompt_n\":%d,\"cache_n\":%d,\"predicted_n\":0}}\n\n", total, cached, evaluated+cached, evaluated, cached)
				}
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}],\"timings\":{\"prompt_n\":%d,\"cache_n\":%d,\"predicted_n\":1}}\n\ndata: [DONE]\n\n", total-cached, cached)
			}))
			defer srv.Close()
			var updates []Progress
			resp, err := NewLlamaClient(srv.URL, "test").Stream(t.Context(), ChatRequest{OnProgress: func(p Progress) { updates = append(updates, p) }}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(updates) != 4 {
				t.Fatalf("updates: %+v", updates)
			}
			for i, p := range updates[:3] {
				processed := cached + (i+1)*512
				if p.Phase != "prefill" || p.PromptTokens != total || p.ProcessedTokens != processed || p.CachedTokens != cached || p.Estimated {
					t.Fatalf("prefill %d: %+v", i, p)
				}
			}
			if resp.Usage.PromptTokens != total || resp.Usage.CachedTokens != cached || resp.Usage.Estimated {
				t.Fatalf("final usage: %+v", resp.Usage)
			}
		})
	}
}

func TestStreamRequestsLocalTelemetryOnlyFromLlama(t *testing.T) {
	for _, local := range []bool{true, false} {
		t.Run(fmt.Sprint(local), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req map[string]any
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				if local {
					if req["return_progress"] != true || req["timings_per_token"] != true {
						t.Error("llama telemetry disabled")
					}
				} else if req["return_progress"] != nil || req["timings_per_token"] != nil {
					t.Error("local fields leaked to remote API")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}],\"timings\":{\"prompt_n\":2,\"predicted_n\":3}}\n\ndata: [DONE]\n\n")
			}))
			defer srv.Close()
			client := NewLlamaClient(srv.URL, "local")
			if !local {
				client = NewOpenAIClient(srv.URL, "remote")
			}
			var last Progress
			req := ChatRequest{Messages: []Message{{Role: RoleUser, Content: strings.Repeat("history ", 100)}}, OnProgress: func(p Progress) { last = p }}
			resp, err := client.Stream(t.Context(), req, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if last.PromptTokens != EstimatePromptTokens(req) || !last.Estimated || last.CompletionTokens != 3 || !resp.Usage.Estimated {
				t.Fatalf("missing cache_n was treated as full input count: %+v usage=%+v", last, resp.Usage)
			}
		})
	}
}

// Deliberation must be reported as it arrives, on its own callback, and
// ahead of the answer. It used to be accumulated into the final message
// only, so a reader saw nothing until the reply was already over.
func TestStream_ReasoningReportedSeparatelyAndFirst(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range []string{
			`data: {"choices":[{"delta":{"reasoning_content":"let me "}}]}`,
			`data: {"choices":[{"delta":{"reasoning_content":"check"}}]}`,
			`data: {"choices":[{"delta":{"content":"done"}}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
		} {
			w.Write([]byte(line + "\n\n"))
		}
	}))
	defer srv.Close()

	c := NewLlamaClient(srv.URL, "test")
	var order []string
	var think, content strings.Builder
	resp, err := c.Stream(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(s string) { think.WriteString(s); order = append(order, "think") },
		func(s string) { content.WriteString(s); order = append(order, "content") })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if think.String() != "let me check" {
		t.Fatalf("reasoning = %q", think.String())
	}
	if content.String() != "done" {
		t.Fatalf("content = %q", content.String())
	}
	if len(order) < 2 || order[0] != "think" || order[len(order)-1] != "content" {
		t.Fatalf("callback order = %v, want reasoning first and answer last", order)
	}
	// The assembled response still carries it for callers that only read
	// the final message.
	if resp.Message.Reasoning != "let me check" {
		t.Fatalf("assembled reasoning = %q", resp.Message.Reasoning)
	}
}

// A nil reasoning callback must not panic: not every backend thinks.
func TestStream_NilCallbacks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(`data: {"choices":[{"delta":{"reasoning_content":"hmm","content":"ok"}}]}` + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := NewLlamaClient(srv.URL, "test")
	resp, err := c.Stream(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, nil, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if resp.Message.Content != "ok" || resp.Message.Reasoning != "hmm" {
		t.Fatalf("assembled = %+v", resp.Message)
	}
}

func TestStream_DeltasAndAssembly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q, want SSE", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range []string{
			`data: {"choices":[{"delta":{"content":"hel"}}]}`,
			`data: {"choices":[{"delta":{"content":"lo"}}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
		} {
			w.Write([]byte(line + "\n\n"))
		}
	}))
	defer srv.Close()

	c := NewLlamaClient(srv.URL, "test")
	var deltas []string
	resp, err := c.Stream(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, nil, func(s string) { deltas = append(deltas, s) })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if strings.Join(deltas, "") != "hello" {
		t.Fatalf("deltas = %q", deltas)
	}
	if resp.Message.Content != "hello" {
		t.Fatalf("assembled = %q", resp.Message.Content)
	}
}

func TestStream_KoboldRefuses(t *testing.T) {
	// Refusal happens before any HTTP dial: unreachable URL proves it.
	c := NewKoboldClient("http://127.0.0.1:1", "x")
	called := false
	_, err := c.Stream(context.Background(), ChatRequest{}, nil, func(string) { called = true })
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unsupported") {
		t.Fatalf("err = %v, want unsupported", err)
	}
	if called {
		t.Fatal("no deltas may flow from a refused stream")
	}
}

func TestStream_EstimatesUsageWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello there, \"}}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"how are you doing today my friend\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := NewLlamaClient(srv.URL, "test")
	resp, err := c.Stream(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hello there, how are you doing today my friend"}},
	}, nil, func(string) {})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if !resp.Usage.Estimated {
		t.Fatal("usage without a block must flag estimated")
	}
	if resp.Usage.PromptTokens <= 0 || resp.Usage.CompletionTokens <= 0 {
		t.Fatalf("usage = %+v, want positive estimates", resp.Usage)
	}
}

func TestStream_KeepsMeasuredUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		w.Write([]byte("data: {\"usage\":{\"prompt_tokens\":2100,\"completion_tokens\":120}}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := NewLlamaClient(srv.URL, "test")
	resp, err := c.Stream(context.Background(), ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, nil, func(string) {})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if resp.Usage.Estimated {
		t.Fatal("measured usage must not flag estimated")
	}
	if resp.Usage.PromptTokens != 2100 || resp.Usage.CompletionTokens != 120 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
}

func TestStreamRejectsIncompleteAndMalformedFrames(t *testing.T) {
	for _, body := range []string{
		`data: {"choices":[{"delta":{"content":"partial"}}]}` + "\n\n",
		"data: invalid\n\ndata: [DONE]\n\n",
		`data: {"error":{"message":"failed"}}` + "\n\ndata: [DONE]\n\n",
	} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer srv.Close()
			c := NewLlamaClient(srv.URL, "test")
			_, err := c.Stream(context.Background(), ChatRequest{}, nil, nil)
			if err == nil {
				t.Fatal("partial/error stream accepted")
			}
		})
	}
}
