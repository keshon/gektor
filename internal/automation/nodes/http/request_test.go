package httpnode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestHTTPResourcesRedactionAndNoRedirect(t *testing.T) {
	calls := 0
	n := definition(func(name, origin string) (string, string, string, error) {
		if name != "api" || origin != "https://api.example.com/items" {
			t.Fatal(name, origin)
		}
		return "Authorization", "Bearer token-secret", "token-secret", nil
	}, transportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Header.Get("Authorization") != "Bearer token-secret" {
			t.Fatal("missing auth")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.example.com"}}, Body: io.NopCloser(strings.NewReader(`{"echo":"token-secret"}`)), Request: req}, nil
	}))
	args := json.RawMessage(`{"url":"https://api.example.com/items","credential":"api"}`)
	resources, err := n.Resources(args)
	if err != nil || len(resources) != 2 || resources[0] != "GET https://api.example.com/items" || resources[1] != "credential:api@https://api.example.com" {
		t.Fatal(resources, err)
	}
	out, err := n.Execute(t.Context(), args)
	if err == nil || calls != 1 || strings.Contains(out, "token-secret") || !strings.Contains(out, "[redacted]") {
		t.Fatal(out, err, calls)
	}
}
func TestHTTPRejectsPrivateDestinationsAndBoundsResponse(t *testing.T) {
	called := false
	n := definition(nil, transportFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("a", (256<<10)+1))), Request: r}, nil
	}))
	for _, url := range []string{"http://127.0.0.1/a", "http://10.0.0.1/a", "file:///x"} {
		raw, _ := json.Marshal(map[string]string{"url": url})
		if _, err := n.Execute(t.Context(), raw); err == nil || called {
			t.Fatal("private request", url)
		}
	}
	if _, err := n.Execute(t.Context(), json.RawMessage(`{"url":"https://api.example.com"}`)); err == nil {
		t.Fatal("oversized response accepted")
	}
}
func TestHTTPPreservesJSONAndCancellation(t *testing.T) {
	n := definition(nil, transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":9007199254740993}`)), Request: r}, nil
	}))
	out, err := n.Execute(t.Context(), json.RawMessage(`{"url":"https://api.example.com"}`))
	if err != nil || !strings.Contains(out, `"data":{"id":9007199254740993}`) {
		t.Fatal(out, err)
	}
	n = definition(nil, transportFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, fmt.Errorf("cancelled")
	}))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := n.Execute(ctx, json.RawMessage(`{"url":"https://api.example.com"}`)); err == nil {
		t.Fatal("ignored cancellation")
	}
}
