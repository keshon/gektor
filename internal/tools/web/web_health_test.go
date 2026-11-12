package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestCheckURL_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tool := Check{}
	args, _ := json.Marshal(map[string]string{"url": srv.URL})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "HTTP\n") || !strings.Contains(out, "status: 200") {
		t.Fatalf("expected structured HTTP header with 200, got: %q", out)
	}
	if !strings.Contains(out, "body-prefix:\nok") {
		t.Fatalf("expected body-prefix with response body, got: %q", out)
	}
}

func TestCheckURL_ConnectionRefused(t *testing.T) {
	tool := Check{}
	// Port 1 is reserved and essentially guaranteed nothing is listening.
	args, _ := json.Marshal(map[string]string{"url": "http://127.0.0.1:1"})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run returned a Go error, want a normal diagnostic result: %v", err)
	}
	if !strings.Contains(out, "error:") {
		t.Fatalf("expected a connection failure message, got: %q", out)
	}
}

// Health probes must not leak credentials: neither check_url nor webfetch
// sends Authorization, Cookie, or Proxy-Authorization, so a malicious
// redirect target or a compromised page cannot harvest the operator's
// tokens. The child-process env is scrubbed separately (see scrubEnv);
// this is the wire half of the same rule.
func TestHealthProbes_SendNoCredentials(t *testing.T) {
	var mu sync.Mutex
	var seen http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<p>hi</p>"))
	}))
	defer srv.Close()

	tool := Check{}
	args, _ := json.Marshal(map[string]string{"url": srv.URL})
	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatalf("check_url: %v", err)
	}
	webfetchTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		seen = r.Header.Clone()
		mu.Unlock()
		return cannedResponse(r, "text/html", "<p>hi</p>")
	})
	defer func() { webfetchTransport = nil }()
	if _, err := wfRun("https://example.com/"); err != nil {
		t.Fatalf("webfetch: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, h := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
		if v := seen.Get(h); v != "" {
			t.Errorf("health probe sent %s: %q", h, v)
		}
	}
}
