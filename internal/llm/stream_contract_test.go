package llm

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPromptProgressDistinguishesUnknownAndZeroCache(t *testing.T) {
	for _, cache := range []string{"", `,"cache":0`, `,"cache":600`} {
		t.Run(cache, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"prompt_progress\":{\"total\":1000,\"processed\":800%s},\"choices\":[]}\n\n", cache)
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer srv.Close()
			var got Progress
			_, err := NewLlamaClient(srv.URL, "test").Stream(t.Context(), ChatRequest{OnProgress: func(p Progress) {
				if p.Phase == "prefill" {
					got = p
				}
			}}, nil, nil)
			if err != nil || got.CacheKnown != (cache != "") || got.ProcessedTokens != 800 {
				t.Fatalf("%+v %v", got, err)
			}
			if strings.Contains(cache, "600") && got.CachedTokens != 600 {
				t.Fatal(got)
			}
		})
	}
}

func TestInterruptedToolStreamRetainsOriginalInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"cut\",\"function\":{\"name\":\"write_file\",\"arguments\":\"{\\\"path\\\":\\\"cut\"}}]}}]}\n\n"))
	}))
	defer srv.Close()
	c := NewLlamaClient(srv.URL, "test")
	resp, err := c.Stream(t.Context(), ChatRequest{Messages: []Message{{Role: RoleUser, Content: "task"}}}, nil, nil)
	if !errors.Is(err, ErrIncompleteStream) || len(resp.Message.ToolCalls) != 1 || !strings.Contains(string(resp.Message.ToolCalls[0].Arguments), "cut") {
		t.Fatalf("partial tool input disappeared: %+v %v", resp, err)
	}
}
