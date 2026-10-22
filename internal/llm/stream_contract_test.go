package llm

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
