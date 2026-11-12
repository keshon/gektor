package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRedirectCannotReachLoopback(t *testing.T) {
	reached := false
	webfetchTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "example.com" {
			resp, _ := cannedResponseCode(r, 302, "text/plain", "")
			resp.Header.Set("Location", "http://127.0.0.1:43210/private")
			return resp, nil
		}
		reached = true
		return cannedResponse(r, "text/plain", "private-data")
	})
	defer func() { webfetchTransport = nil }()
	out, err := (Fetch{}).Run(context.Background(), json.RawMessage(`{"url":"https://example.com"}`))
	if err == nil || reached || strings.Contains(out, "private-data") {
		t.Fatalf("repro failed: %v %s", err, out)
	}

}

func TestWebfetchRejectsNonIntegerLimit(t *testing.T) {
	_, err := (Fetch{}).Run(context.Background(), json.RawMessage(`{"url":"https://example.com","max_bytes":"100"}`))
	if err == nil || !strings.Contains(err.Error(), "cannot unmarshal string") {
		t.Fatalf("repro failed: %v", err)
	}
	t.Log(err)
}
