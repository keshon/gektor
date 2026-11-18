package httpnode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/httptransport"
	"github.com/keshon/gektor/internal/workflow"
)

type redirectAuthorizer func(context.Context, string, json.RawMessage) error

func (f redirectAuthorizer) AuthorizeCall(ctx context.Context, name string, args json.RawMessage) error {
	return f(ctx, name, args)
}

func TestRedirectsRejectPrivateCrossOriginCredentialsAndMissingAuthorization(t *testing.T) {
	for _, test := range []struct {
		name, location, credential string
		deniedDestination          bool
	}{
		{"private", "http://127.0.0.1/admin", "", true},
		{"cross-origin credential", "https://other.example.com/next", ",\"credential\":\"api\",\"credential_revision\":\"" + testRevision + "\"", true},
		{"missing authorizer", "/next", "", false},
		{"credential in redirect query", "/next?token=private-token", ",\"credential\":\"api\",\"credential_revision\":\"" + testRevision + "\"", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			node := definition(func(string, string, string) (string, string, string, error) {
				return "Authorization", "Bearer private-token", "private-token", nil
			}, transportFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{test.location}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
			}))
			raw := json.RawMessage(`{"url":"https://api.example.com/start","max_redirects":1` + test.credential + `}`)
			_, err := node.Execute(t.Context(), raw)
			if err == nil || calls != 1 || workflow.OperationFailure(err) != nil {
				t.Fatal("unsafe redirect dispatched or became routable", err, calls)
			}
			if test.deniedDestination && !errors.Is(err, httptransport.ErrDestinationDenied) {
				t.Fatal("destination rejection lost identity", err)
			}
		})
	}
}

func TestRedirectBudgetAnd307BodyAndCredentials(t *testing.T) {
	for _, loop := range []bool{false, true} {
		t.Run(fmt.Sprint("loop=", loop), func(t *testing.T) {
			calls, resolutions, authorizations := 0, 0, 0
			node := definition(func(string, string, string) (string, string, string, error) {
				resolutions++
				secret := fmt.Sprintf("private-token-%d", resolutions)
				return "Authorization", "Bearer " + secret, secret, nil
			}, transportFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				expected := fmt.Sprintf("Bearer private-token-%d", calls)
				if request.Method != "POST" || string(body) != `{"value":1}` || request.Header.Get("Authorization") != expected {
					t.Fatal("307 changed payload or kept stale credential", request.Method, string(body), request.Header)
				}
				status := 200
				headers := http.Header{"X-Echo": []string{"private-token-1 private-token-2"}}
				if calls == 1 || loop {
					status = 307
					headers.Set("Location", "/next")
				}
				return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(`{}`)), Request: request}, nil
			}))
			gate := redirectAuthorizer(func(_ context.Context, name string, args json.RawMessage) error {
				authorizations++
				current, err := parse(args)
				if err != nil || name != "http_request" || current.URL != "https://api.example.com/next" {
					t.Fatal(name, current.URL, err)
				}
				return nil
			})
			ctx := workflow.WithCallAuthorizer(t.Context(), gate)
			raw := json.RawMessage(`{"url":"https://api.example.com/start","method":"POST","body":{"value":1},"max_redirects":2,"credential":"api","credential_revision":"` + testRevision + `","response_headers":["X-Echo"]}`)
			out, err := node.Execute(ctx, raw)
			if loop {
				failure := workflow.OperationFailure(err)
				if failure == nil || failure.Code != "http_redirect_limit" || calls != 3 || authorizations != 2 {
					t.Fatal(out, err, calls, authorizations)
				}
				return
			}
			if err != nil || calls != 2 || resolutions != 2 || authorizations != 1 || strings.Contains(out, "private-token") {
				t.Fatal(out, err, calls, resolutions, authorizations)
			}
		})
	}
}
