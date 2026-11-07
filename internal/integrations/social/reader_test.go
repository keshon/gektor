package social

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnsupportedURLNeverReachesProvider(t *testing.T) {
	for _, raw := range []string{"https://instagram.com/p/123", "https://x.com.evil/status/20", "https://x.com@evil.test/u/status/20", "http://x.com/u/status/20", "https://x.com:123/u/status/20", "https://x.com/u/status/20/../21"} {
		if _, err := postID(raw); err == nil {
			t.Fatal(raw)
		}
	}
	p := ReadPost(t.Context(), nil, "unused", "https://example.org/private")
	if p.Status != "unsupported" {
		t.Fatal(p)
	}
}

func TestCancellationAndTruncationRemainExplicit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 200, "tweet": map[string]any{"text": strings.Repeat("long text", 30000), "author": map[string]string{"screen_name": "test"}}})
	}))
	defer server.Close()
	p := ReadPost(t.Context(), server.Client(), server.URL, "https://x.com/test/status/20")
	encoded, _ := json.Marshal(p)
	if p.Status != "truncated" || !p.Truncated || len(encoded) > 40*1024 {
		t.Fatal(p.Status, len(encoded))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if p := ReadPost(ctx, server.Client(), server.URL, "https://x.com/test/status/20"); p.Status != "transient_error" {
		t.Fatal(p)
	}
}
