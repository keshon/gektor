package httpnode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestRequestFormatsAndQueryUseTheAuthorizedURL(t *testing.T) {
	for _, test := range []struct{ format, body, want, contentType string }{
		{"json", `{"name":"item"}`, `{"name":"item"}`, "application/json"},
		{"json", `{"name":"item"}`, `{"name":"item"}`, "application/merge-patch+json"},
		{"text", `"plain & text"`, "plain & text", "text/plain; charset=utf-8"},
		{"text", `""`, "", "text/plain; charset=utf-8"},
		{"form", `{"tag":["a b","c"],"name":["x&y"]}`, "name=x%26y&tag=a+b&tag=c", "application/x-www-form-urlencoded"},
	} {
		t.Run(test.format, func(t *testing.T) {
			raw := json.RawMessage(`{"url":"https://api.example.com/items?keep=yes&tag=old","method":"POST","query":{"tag":["a b","c"]},"body_format":"` + test.format + `","body":` + test.body + `}`)
			if test.contentType == "application/merge-patch+json" {
				raw = json.RawMessage(strings.TrimSuffix(string(raw), "}") + `,"headers":{"Content-Type":"application/merge-patch+json"}}`)
			}
			node := definition(nil, transportFunc(func(request *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				if string(body) != test.want || request.Header.Get("Content-Type") != test.contentType || request.URL.RawQuery != "keep=yes&tag=a+b&tag=c" {
					t.Fatal(string(body), request.Header, request.URL)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: request}, nil
			}))
			resources, err := node.Resources(raw)
			if err != nil || resources[0] != "POST https://api.example.com/items?keep=yes&tag=a+b&tag=c" {
				t.Fatal(resources, err)
			}
			_, err = node.Execute(t.Context(), raw)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestResponseModesDistinguishJSONTextAndSoftErrors(t *testing.T) {
	for _, test := range []struct {
		format, body, code string
		data               bool
	}{
		{"auto", `{"ok":true}`, "", true},
		{"text", `{"ok":true}`, "", false},
		{"auto", `{"success":false,"error":"not found"}`, "", true},
		{"json", `{"id":9007199254740993}`, "", true},
		{"json", "<html>not found</html>", "http_response_invalid_json", false},
		{"text", string([]byte{255}), "http_response_encoding", false},
	} {
		t.Run(test.format+test.code, func(t *testing.T) {
			node := definition(nil, transportFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Etag": []string{"version-1"}}, Body: io.NopCloser(strings.NewReader(test.body)), Request: request}, nil
			}))
			raw := json.RawMessage(`{"url":"https://api.example.com","response_format":"` + test.format + `","response_headers":["ETag"]}`)
			out, err := node.Execute(t.Context(), raw)
			if test.code != "" {
				failure := workflow.OperationFailure(err)
				if failure == nil || failure.Code != test.code {
					t.Fatal(out, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result responseResult
			err = json.Unmarshal([]byte(out), &result)
			if err != nil || (len(result.Data) > 0) != test.data || result.Headers["etag"] != "version-1" {
				t.Fatal(out, err)
			}
		})
	}
	node := definition(nil, nil)
	for _, raw := range []string{
		`{"url":"https://api.example.com","response_headers":["Set-Cookie"]}`,
		`{"url":"https://api.example.com/a b"}`,
		`{"url":"https://api.example.com/a\\b"}`,
		`{"url":"https://api.example.com","response_format":"artifact"}`,
		`{"url":"https://api.example.com","body_format":"artifact","body_artifact":"source","body":{}}`,
		`{"url":"https://api.example.com","max_response_bytes":262145}`,
	} {
		err := node.Validate(json.RawMessage(raw))
		if err == nil {
			t.Fatal("invalid mode accepted", raw)
		}
	}
}

func TestArtifactsStreamBinaryAndProvideRequestBodies(t *testing.T) {
	db, err := store.OpenStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	writer, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	access := writer.Artifacts("request")
	payload := bytes.Repeat([]byte{0, 255, 1, 2}, 150000)
	source, err := access.Put(t.Context(), "source.bin", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	ctx := workflow.WithArtifacts(t.Context(), access)
	node := definition(nil, transportFunc(func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil || !bytes.Equal(body, payload) {
			t.Fatal("artifact upload changed bytes", readErr)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(payload)), Request: request}, nil
	}))
	raw := json.RawMessage(`{"url":"https://api.example.com","method":"POST","body_format":"artifact","body_artifact":"` + source.ID + `","response_format":"artifact","artifact_name":"response.bin"}`)
	resources, err := node.Resources(raw)
	if err != nil || len(resources) != 3 || resources[1] != source.ID || resources[2] != "response.bin" {
		t.Fatal(resources, err)
	}
	out, err := node.Execute(ctx, raw)
	if err != nil || len(out) > 2048 {
		t.Fatal(out, err)
	}
	var result responseResult
	err = json.Unmarshal([]byte(out), &result)
	if err != nil || result.Artifact == nil {
		t.Fatal(out, err)
	}
	_, saved, err := access.Read(t.Context(), result.Artifact.ID)
	if err != nil || !bytes.Equal(saved, payload) {
		t.Fatal("artifact download changed bytes", err)
	}

	before, err := writer.ListArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	raw = json.RawMessage(`{"url":"https://api.example.com","method":"POST","body_format":"artifact","body_artifact":"` + source.ID + `","response_format":"artifact","artifact_name":"too-large.bin","max_response_bytes":10}`)
	_, err = node.Execute(ctx, raw)
	if err == nil {
		t.Fatal("oversized response published")
	}
	after, err := writer.ListArtifacts()
	if err != nil || len(after) != len(before) {
		t.Fatal("failed stream retained published bytes", err)
	}
}

func TestArtifactCredentialGuardHandlesReadBoundaries(t *testing.T) {
	secret := `private"credential`
	var secrets redactor
	err := secrets.add(secret)
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range secrets.patterns {
		body := strings.Repeat("x", (32<<10)-3) + pattern + "tail"
		reader := &artifactBody{source: strings.NewReader(body), remaining: artifactLimit, patterns: secrets.patterns}
		for _, value := range secrets.patterns {
			reader.keep = max(reader.keep, len(value)-1)
		}
		_, err = io.ReadAll(reader)
		if err == nil {
			t.Fatal("credential spanning a read boundary accepted")
		}
	}
}

func TestRedirectsUseCurrentServicePolicyBeforeEveryDestination(t *testing.T) {
	for _, deny := range []bool{false, true} {
		t.Run(fmt.Sprint("deny=", deny), func(t *testing.T) {
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			var visited []string
			node := definition(nil, transportFunc(func(request *http.Request) (*http.Response, error) {
				visited = append(visited, request.URL.Path)
				if request.URL.Path == "/start" {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"/next"}}, Body: io.NopCloser(strings.NewReader("ignored")), Request: request}, nil
				}
				if request.Method != "GET" || request.ContentLength != 0 {
					t.Fatal("POST body was replayed after 302")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"done":true}`)), Request: request}, nil
			}))
			registry, err := workflow.NewRegistry(node)
			if err != nil {
				t.Fatal(err)
			}
			var authorized []string
			svc := service.Service{WS: ws, StoreDir: t.TempDir(), Registry: registry, AuthorizeResource: func(_ context.Context, _ string, target string, _ json.RawMessage) error {
				authorized = append(authorized, target)
				if deny && strings.Contains(target, "/next") {
					return fmt.Errorf("next destination denied")
				}
				return nil
			}}
			raw := []byte(`{"version":4,"name":"redirect","steps":[{"id":"request","tool":"http_request","args":{"url":"https://api.example.com/start","method":"POST","body":{"value":1},"max_redirects":2}}]}`)
			out, err := svc.RunSource(t.Context(), raw, "redirect.json", nil)
			if (err != nil) != deny {
				t.Fatal(out, err)
			}
			if deny && workflow.OperationFailure(err) != nil {
				t.Fatal("permission denial became routable", err)
			}
			expected := 2
			if deny {
				expected = 1
			}
			if len(visited) != expected {
				t.Fatal("unauthorized destination fetched", visited)
			}
			if len(authorized) < 2 || authorized[len(authorized)-1] != "GET https://api.example.com/next" {
				t.Fatal(authorized)
			}
			if deny && out.Status != "failed" {
				t.Fatal("denial was hidden", out)
			}
		})
	}
}

func TestFailedArtifactHTTPResponsesPublishNothing(t *testing.T) {
	for _, mode := range []string{"secret", "read", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			db, err := store.OpenStore(t.TempDir(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			writer, err := db.NewRun()
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			secret := `private"credential`
			node := definition(func(string, string, string) (string, string, string, error) {
				return "Authorization", "Bearer " + secret, secret, nil
			}, transportFunc(func(request *http.Request) (*http.Response, error) {
				var body io.ReadCloser = io.NopCloser(strings.NewReader(strings.Repeat("x", (32<<10)-3) + secret))
				if mode == "read" {
					body = brokenBody{}
				}
				if mode == "cancel" {
					body = cancelledBody{cause: context.Canceled}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: body, Request: request}, nil
			}))
			ctx := workflow.WithArtifacts(t.Context(), writer.Artifacts("download"))
			raw := json.RawMessage(`{"url":"https://api.example.com","credential":"api","credential_revision":"` + testRevision + `","response_format":"artifact","artifact_name":"response.bin"}`)
			out, err := node.Execute(ctx, raw)
			if err == nil || out != "" || workflow.OperationFailure(err) != nil || strings.Contains(err.Error(), secret) {
				t.Fatal("artifact failure became success, leaked, or became routable", out, err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("artifact lost cancellation identity", err)
			}
			published, err := writer.ListArtifacts()
			if err != nil || len(published) != 0 {
				t.Fatal("failed response published an artifact", published, err)
			}
		})
	}
}

func TestHTTPWorkflowPassesBinaryArtifactAcrossReview(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	payload := bytes.Repeat([]byte{0, 255, 1, 2}, 100000)
	downloads, uploads := 0, 0
	node := definition(nil, transportFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == "GET" {
			downloads++
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(payload)), Request: request}, nil
		}
		uploads++
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil || !bytes.Equal(body, payload) {
			t.Fatal("workflow artifact changed", readErr)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"accepted":true}`)), Request: request}, nil
	}))
	registry, err := workflow.NewRegistry(node)
	if err != nil {
		t.Fatal(err)
	}
	svc := service.Service{WS: ws, StoreDir: t.TempDir(), Registry: registry, AuthorizeResource: func(context.Context, string, string, json.RawMessage) error { return nil }}
	recipe := []byte(`{"version":4,"name":"binary transfer","steps":[{"id":"download","tool":"http_request","args":{"url":"https://api.example.com/file","response_format":"artifact","artifact_name":"source.bin"}},{"id":"upload","tool":"http_request","review":true,"depends_on":["download"],"args":{"url":"https://api.example.com/upload","method":"POST","body_format":"artifact","body_artifact":{"$step":"download","pointer":"/artifact/id"},"response_format":"json"}}]}`)
	out, err := svc.RunSource(t.Context(), recipe, "binary.json", nil)
	if err != nil || downloads != 1 || uploads != 0 || out.Status != "waiting_review" {
		t.Fatal(out, err, downloads, uploads)
	}
	summary := out
	restarted := service.Service{WS: ws, StoreDir: svc.StoreDir, Registry: registry, AuthorizeResource: svc.AuthorizeResource}
	out, err = restarted.Review(t.Context(), summary.RunID, "upload", true)
	if err != nil || downloads != 1 || uploads != 1 || out.Status != "completed" {
		t.Fatal(out, err, downloads, uploads)
	}
}

func TestArtifactResponseRequiresRunAccessBeforeDispatch(t *testing.T) {
	called := false
	node := definition(nil, transportFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, fmt.Errorf("must not dispatch")
	}))
	_, err := node.Execute(t.Context(), json.RawMessage(`{"url":"https://api.example.com","method":"POST","response_format":"artifact","artifact_name":"response.bin"}`))
	if err == nil || called {
		t.Fatal("request dispatched without its required artifact owner", err)
	}
}

func TestFormLimitAppliesAfterURLEncoding(t *testing.T) {
	raw := json.RawMessage(`{"url":"https://api.example.com","body_format":"form","body":{"field":["` + strings.Repeat("?", 22000) + `"]}}`)
	node := Definition(nil)
	err := node.Validate(raw)
	if err == nil {
		t.Fatal("URL encoding bypassed inline request limit")
	}
}
