package httpnode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
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

func TestHTTPStatusAsDataAndSelectedHeaders(t *testing.T) {
	for _, status := range []int{200, 302, 401, 429, 503} {
		for _, asData := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/as_data=%t", status, asData), func(t *testing.T) {
				calls := 0
				n := definition(func(string, string) (string, string, string, error) {
					return "Authorization", "Bearer fixture-secret", "fixture-secret", nil
				}, transportFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					headers := http.Header{}
					headers.Set("Retry-After", "120")
					headers.Set("Content-Type", "application/json")
					headers.Set("X-RateLimit-Limit", "fixture-secret")
					headers.Set("X-RateLimit-Remaining", "0")
					headers.Set("X-RateLimit-Reset", "12345")
					headers.Set("Set-Cookie", "private-cookie")
					headers.Set("Location", "https://other.example.com")
					return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(`{"message":"fixture-secret"}`)), Request: r}, nil
				}))
				raw := json.RawMessage(fmt.Sprintf(`{"url":"https://api.example.com/items","credential":"api","status_as_data":%t}`, asData))
				if err := workflow.ValidateJSON(raw, n.InputSchema); err != nil {
					t.Fatal(err)
				}
				out, err := n.Execute(t.Context(), raw)
				if (err != nil) != (!asData && (status < 200 || status >= 300)) || calls != 1 {
					t.Fatal(out, err, calls)
				}
				var result struct {
					Status  int               `json:"status_code"`
					Headers map[string]string `json:"headers"`
					Data    map[string]string `json:"data"`
				}
				if err := json.Unmarshal([]byte(out), &result); err != nil {
					t.Fatal(err)
				}
				if result.Status != status || len(result.Headers) != 5 || result.Headers["retry-after"] != "120" || result.Headers["x-ratelimit-remaining"] != "0" || result.Headers["x-ratelimit-reset"] != "12345" || result.Headers["x-ratelimit-limit"] != "[redacted]" || result.Data["message"] != "[redacted]" || strings.Contains(out, "private-cookie") || strings.Contains(out, "fixture-secret") {
					t.Fatal(out)
				}
			})
		}
	}
}

type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, fmt.Errorf("fixture read failure") }
func (brokenBody) Close() error             { return nil }

func TestHTTPStatusAsDataDoesNotHideTransportOrResponseErrors(t *testing.T) {
	for _, failure := range []string{"transport", "read", "body-size", "header-size"} {
		t.Run(failure, func(t *testing.T) {
			n := definition(nil, transportFunc(func(r *http.Request) (*http.Response, error) {
				if failure == "transport" {
					return nil, fmt.Errorf("fixture transport failure")
				}
				res := &http.Response{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}
				switch failure {
				case "read":
					res.Body = brokenBody{}
				case "body-size":
					res.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", (256<<10)+1)))
				case "header-size":
					res.Header.Set("Retry-After", strings.Repeat("x", 4097))
				}
				return res, nil
			}))
			if _, err := n.Execute(t.Context(), json.RawMessage(`{"url":"https://api.example.com","status_as_data":true}`)); err == nil {
				t.Fatal("error was hidden")
			}
		})
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
