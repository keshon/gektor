package httpnode

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/credentials"
	artifactnode "github.com/keshon/gektor/internal/workflow/nodes/artifact"
	waitnodes "github.com/keshon/gektor/internal/workflow/nodes/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestReviewedHTTPBindingResolvesCurrentSecretsAndRejectsChanges(t *testing.T) {
	for _, mode := range []string{"rotate", "revoke", "rebind"} {
		t.Run(mode, func(t *testing.T) {
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			t.Setenv("GEKTOR_HTTP_BINDING_TEST", "first-private-value")
			bindings := credentials.Store{Directory: t.TempDir()}
			config := credentials.Config{Origin: "https://api.example.com", Env: "GEKTOR_HTTP_BINDING_TEST", Header: "Authorization", Prefix: "Bearer "}
			err = bindings.Register("api", config)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := bindings.Inspect("api")
			if err != nil {
				t.Fatal(err)
			}
			requests := 0
			node := definition(bindings.Resolve, transportFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.Header.Get("Authorization") != "Bearer rotated-private-value" {
					t.Fatal("request used stale secret")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
			}))
			nodes := append(artifactnode.Definitions(ws), node)
			registry, err := workflow.NewRegistry(nodes...)
			if err != nil {
				t.Fatal(err)
			}
			args, err := json.Marshal(struct {
				URL        string `json:"url"`
				Credential string `json:"credential"`
				Revision   string `json:"credential_revision"`
			}{URL: config.Origin + "/items", Credential: "api", Revision: binding.Revision})
			if err != nil {
				t.Fatal(err)
			}
			definition := workflow.Definition{Version: 4, Name: "review binding", Steps: []workflow.Step{
				{ID: "request", Tool: "http_request", Args: args, Review: true},
				{ID: "report", Tool: "artifact_write", OnFailure: "request", Args: json.RawMessage(`{"name":"failure.txt","text":"failed"}`)},
				{ID: "finish", Tool: "artifact_write", Finally: true, Args: json.RawMessage(`{"name":"finished.txt","text":"done"}`)},
			}}
			source, err := json.Marshal(definition)
			if err != nil {
				t.Fatal(err)
			}
			var resources []string
			runID := ""
			svc := service.Service{WS: ws, StoreDir: t.TempDir(), Registry: registry,
				OnRun: func(id string) error { runID = id; return nil },
				AuthorizeResource: func(_ context.Context, _ string, resource string, _ json.RawMessage) error {
					resources = append(resources, resource)
					return nil
				},
			}
			out, err := svc.RunSource(t.Context(), source, "http.json", nil)
			if err != nil || requests != 0 || !strings.Contains(out, "waiting_review") {
				t.Fatal(out, err, requests)
			}
			target := "credential:api@https://api.example.com#" + binding.Revision

			switch mode {
			case "rotate":
				t.Setenv(config.Env, "rotated-private-value")
			case "revoke":
				err = bindings.Revoke("api")
			case "rebind":
				config.Prefix = "Changed "
				data, encodeErr := json.Marshal(config)
				if encodeErr != nil {
					t.Fatal(encodeErr)
				}
				err = os.WriteFile(filepath.Join(bindings.Directory, "api.json"), data, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			out, err = svc.Review(t.Context(), runID, "request", true)
			if !slices.Contains(resources, target) {
				t.Fatal("permission omitted binding revision", resources)
			}
			if (err == nil) != (mode == "rotate") || workflow.OperationFailure(err) != nil {
				t.Fatal(out, err)
			}
			if (requests == 1) != (mode == "rotate") {
				t.Fatal("request dispatched with invalid binding", requests)
			}
			db, err := store.OpenStore(svc.StoreDir, ws.Root())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			state, err := db.Run(runID)
			if err != nil {
				t.Fatal(err)
			}
			record, err := json.Marshal(state.Record)
			if err != nil || strings.Contains(string(record), "first-private-value") || strings.Contains(string(record), "rotated-private-value") {
				t.Fatal("secret in checkpoint", err)
			}
			if mode != "rotate" && (state.Record.Steps[1].Status != "skipped" || state.Record.Steps[2].Status != "skipped") {
				t.Fatal("binding error dispatched route or finalizer")
			}
		})
	}
}

func TestHTTPRequiresPinnedCredentialPair(t *testing.T) {
	node := Definition(nil)
	for _, raw := range []string{
		`{"url":"https://api.example.com","credential":"api"}`,
		`{"url":"https://api.example.com","credential_revision":"` + testRevision + `"}`,
		`{"url":"https://api.example.com","credential":"api","credential_revision":"invalid"}`,
	} {
		args := json.RawMessage(raw)
		err := workflow.ValidateJSON(args, node.InputSchema)
		if err == nil {
			t.Fatal("schema accepted unpinned credential")
		}
		err = node.Validate(args)
		if err == nil {
			t.Fatal("node accepted unpinned credential")
		}
	}
}

func TestCallbackHTTPWorkflowRestartsAndChecksCurrentCredentials(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "rotate", true: "revoke"}[revoke], func(t *testing.T) {
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			var requests atomic.Int32
			endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				number := requests.Add(1)
				expected := "Bearer initial-private-value"
				if number == 2 {
					expected = "Bearer rotated-private-value"
				}
				if r.Header.Get("Authorization") != expected {
					t.Error("request used stale credential")
				}
				w.Header().Set("Content-Type", "application/json")
				_, writeErr := w.Write([]byte(`{"accepted":true}`))
				if writeErr != nil {
					t.Error(writeErr)
				}
			}))
			defer endpoint.Close()
			t.Setenv("GEKTOR_CALLBACK_HTTP_SECRET", "initial-private-value")
			bindings := credentials.Store{Directory: t.TempDir()}
			err = bindings.Register("api", credentials.Config{Origin: "https://api.example.com", Env: "GEKTOR_CALLBACK_HTTP_SECRET", Header: "Authorization", Prefix: "Bearer "})
			if err != nil {
				t.Fatal(err)
			}
			binding, err := bindings.Inspect("api")
			if err != nil {
				t.Fatal(err)
			}
			storeDir := t.TempDir()
			newService := func() service.Service {
				current := credentials.Store{Directory: bindings.Directory}
				nodes := waitnodes.Definitions()
				// Only the test transport routes this public origin to the local TLS fixture.
				target, parseErr := url.Parse(endpoint.URL)
				if parseErr != nil {
					t.Fatal(parseErr)
				}
				transport := transportFunc(func(request *http.Request) (*http.Response, error) {
					routed := request.Clone(request.Context())
					routed.URL.Scheme, routed.URL.Host = target.Scheme, target.Host
					return endpoint.Client().Transport.RoundTrip(routed)
				})
				nodes = append(nodes, definition(current.Resolve, transport))
				registry, registryErr := workflow.NewRegistry(nodes...)
				if registryErr != nil {
					t.Fatal(registryErr)
				}
				return service.Service{WS: ws, StoreDir: storeDir, Registry: registry, AuthorizeResource: func(context.Context, string, string, json.RawMessage) error { return nil }}
			}
			args, err := json.Marshal(struct {
				URL        string `json:"url"`
				Credential string `json:"credential"`
				Revision   string `json:"credential_revision"`
			}{"https://api.example.com", "api", binding.Revision})
			if err != nil {
				t.Fatal(err)
			}
			recipe := workflow.Definition{Version: 4, Name: "callback HTTP acceptance", Steps: []workflow.Step{
				{ID: "before", Tool: "http_request", Args: args},
				{ID: "wait", Tool: "workflow_wait_callback", DependsOn: []string{"before"}, Args: json.RawMessage(`{"channel":"approval","timeout_ms":60000}`)},
				{ID: "after", Tool: "http_request", DependsOn: []string{"wait"}, Args: args, Review: true},
			}}
			raw, err := json.Marshal(recipe)
			if err != nil {
				t.Fatal(err)
			}
			first := newService()
			out, err := first.RunSource(t.Context(), raw, "callback-http.json", nil)
			if err != nil || requests.Load() != 1 || !strings.Contains(out, `"waiting"`) {
				t.Fatal(out, err)
			}
			var summary struct {
				ID string `json:"run_id"`
			}
			err = json.Unmarshal([]byte(out), &summary)
			if err != nil {
				t.Fatal(err)
			}
			db, err := store.OpenStore(storeDir, ws.Root())
			if err != nil {
				t.Fatal(err)
			}
			state, err := db.Run(summary.ID)
			if err != nil {
				db.Close()
				t.Fatal(err)
			}
			receipt := workflow.PendingCallback(state.Record)
			if receipt == nil {
				db.Close()
				t.Fatal("missing receipt")
			}
			delivery := store.CallbackDelivery{RunID: summary.ID, WaitID: receipt.ID, Channel: receipt.Channel, EventID: "accepted-once", Payload: json.RawMessage(`{"approved":true}`)}
			duplicate, err := db.AcceptCallback(t.Context(), delivery)
			if err != nil || duplicate {
				db.Close()
				t.Fatal(duplicate, err)
			}
			duplicate, err = db.AcceptCallback(t.Context(), delivery)
			if err != nil || !duplicate {
				db.Close()
				t.Fatal("callback was not deduplicated", err)
			}
			err = db.Close()
			if err != nil {
				t.Fatal(err)
			}

			// Reopen the store and reconstruct the registry before consuming the callback.
			db, err = store.OpenStore(storeDir, ws.Root())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			writer, record, claimed, err := db.ClaimWait(t.Context(), time.Now())
			if err != nil || !claimed {
				t.Fatal(claimed, err)
			}
			restarted := newService()
			err = restarted.ContinueWait(t.Context(), db, writer, record)
			closeErr := writer.Close()
			if err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
			state, err = db.Run(summary.ID)
			if err != nil || state.Status != "waiting_review" || requests.Load() != 1 {
				t.Fatal(state.Status, err)
			}
			if revoke {
				err = bindings.Revoke("api")
			} else {
				t.Setenv("GEKTOR_CALLBACK_HTTP_SECRET", "rotated-private-value")
			}
			if err != nil {
				t.Fatal(err)
			}
			reviewed := newService()
			out, err = reviewed.Review(t.Context(), summary.ID, "after", true)
			if (err == nil) == revoke {
				t.Fatal(out, err)
			}
			expectedCalls := int32(2)
			if revoke {
				expectedCalls = 1
			}
			if requests.Load() != expectedCalls {
				t.Fatal("replayed or unauthorized request", requests.Load())
			}
			state, err = db.Run(summary.ID)
			if err != nil {
				t.Fatal(err)
			}
			checkpoint, err := json.Marshal(state.Record)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(checkpoint), "private-value") {
				t.Fatal("checkpoint retained a secret")
			}
		})
	}
}
