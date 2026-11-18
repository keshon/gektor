package httpnode

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/credentials"
	artifactnode "github.com/keshon/gektor/internal/workflow/nodes/artifact"
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
