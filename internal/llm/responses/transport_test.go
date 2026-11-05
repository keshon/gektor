package responses

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

func TestResponseHeaderWaitIsBounded(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	c := newTestClient(server.URL, "test")
	c.http = responseHTTPClient()
	c.http.Transport.(*http.Transport).ResponseHeaderTimeout = 30 * time.Millisecond
	var mu sync.Mutex
	var phases []string
	req := llm.ChatRequest{OnProgress: func(p llm.Progress) { mu.Lock(); defer mu.Unlock(); phases = append(phases, p.Subphase) }}
	_, err := c.sendResponses(t.Context(), req, []byte(`{}`), "test", nil, nil)
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("expected header timeout, got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(phases) < 2 || phases[0] != "sending" || phases[1] != "headers" {
		t.Fatal(phases)
	}
}

func TestSilentStreamTimeoutAndCancellation(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "cancel"}[cancelEarly], func(t *testing.T) {
			entered := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				close(entered)
				<-r.Context().Done()
			}))
			defer server.Close()
			c := newTestClient(server.URL, "test")
			c.streamIdleTimeout = 40 * time.Millisecond
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if cancelEarly {
				go func() { <-entered; cancel() }()
			}
			_, err := c.sendResponses(ctx, llm.ChatRequest{}, []byte(`{}`), "test", nil, nil)
			expected := context.DeadlineExceeded
			if cancelEarly {
				expected = context.Canceled
			}
			if !errors.Is(err, expected) {
				t.Fatalf("got %v, expected %v", err, expected)
			}
		})
	}
}

func TestStreamActivityRenewsIdleDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for range 6 {
			_, _ = w.Write([]byte(": keepalive\n\n"))
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"))
	}))
	defer server.Close()
	c := newTestClient(server.URL, "test")
	c.streamIdleTimeout = 100 * time.Millisecond
	_, err := c.sendResponses(t.Context(), llm.ChatRequest{}, []byte(`{}`), "test", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
}
