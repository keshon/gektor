package httpnode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workflow"
	artifactnode "github.com/keshon/gektor/internal/workflow/nodes/artifact"
	modelnode "github.com/keshon/gektor/internal/workflow/nodes/model"
	"github.com/keshon/gektor/internal/workflow/nodes/record"
	scriptnode "github.com/keshon/gektor/internal/workflow/nodes/script"
	"github.com/keshon/gektor/internal/workflow/script"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "workflow" && os.Args[2] == "internal-script-worker" {
		if err := script.Serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type draftClient struct {
	calls int
	fail  bool
}

func (c *draftClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.calls++
	if len(req.Tools) != 0 || len(req.Messages) != 1 || !strings.Contains(req.Messages[0].Content, "untrusted") || !strings.Contains(req.Messages[0].Content, "post-1") {
		return llm.ChatResponse{}, fmt.Errorf("unexpected draft request")
	}
	if c.fail {
		return llm.ChatResponse{}, fmt.Errorf("fixture model unavailable")
	}
	return llm.ChatResponse{Message: llm.Message{Content: `{"should_post":true,"title":"A draft","content":"A local draft based on the supplied posts.","reason":"Useful topic."}`}, FinishReason: "stop"}, nil
}

func TestMoltbookDraftRecipe(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		status     int
		body       string
		modelFails bool
		outcome    string
		drafts     int
	}{
		{"draft-and-repeat", 200, `{"success":true,"posts":[{"id":"post-1","title":"State","content":"Keep state explicit."}]}`, false, "draft_saved", 1},
		{"empty-feed", 200, `{"success":true,"posts":[]}`, false, "no_new_posts", 0},
		{"rate-limit", 429, `{"error":"slow down"}`, false, "http_error", 0},
		{"unauthorized", 401, `{"error":"unauthorized"}`, false, "http_error", 0},
		{"server-error", 503, `{"error":"unavailable"}`, false, "http_error", 0},
		{"bad-feed", 200, `{"success":true}`, false, "", 0},
		{"model-failure", 200, `{"success":true,"posts":[{"id":"post-1","title":"State","content":"Keep state explicit."}]}`, true, "", 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			directory := t.TempDir()
			db, err := store.OpenStore(directory, ws.Root())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			fixture := filepath.Join("..", "..", "..", "..", "examples", "workflows", "moltbook-draft")
			schema, err := os.ReadFile(filepath.Join(fixture, "state.schema.json"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.RegisterSchema(t.Context(), "feed_state", schema); err != nil {
				t.Fatal(err)
			}
			source, err := os.ReadFile(filepath.Join(fixture, "recipe.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := ws.WriteFile("recipe.json", source, 0600); err != nil {
				t.Fatal(err)
			}
			client := &draftClient{fail: scenario.modelFails}
			httpCalls := 0
			fetch := definition(nil, transportFunc(func(req *http.Request) (*http.Response, error) {
				httpCalls++
				if req.Method != "GET" {
					t.Fatal("recipe tried to publish")
				}
				headers := http.Header{}
				if scenario.status == 429 {
					headers.Set("Retry-After", "120")
				}
				return &http.Response{StatusCode: scenario.status, Header: headers, Body: io.NopCloser(strings.NewReader(scenario.body)), Request: req}, nil
			}))
			generate := modelnode.Definition(func(_ context.Context, path string) (modelnode.Connection, error) {
				if path != "model.json" {
					return modelnode.Connection{}, fmt.Errorf("unexpected profile")
				}
				return modelnode.Connection{Client: client, Name: "fixture", MaxTokens: 2048, ContextLimit: 16384}, nil
			})
			nodes := append(record.Definitions(ws, directory), artifactnode.Definitions(ws)...)
			nodes = append(nodes, fetch, generate, scriptnode.Definition())
			registry, err := workflow.NewRegistry(nodes...)
			if err != nil {
				t.Fatal(err)
			}
			runID := ""
			svc := service.Service{WS: ws, StoreDir: directory, Registry: registry, OnRun: func(id string) error { runID = id; return nil }, Authorize: func(context.Context, string, json.RawMessage) error { return nil }}
			out, err := svc.Run(t.Context(), json.RawMessage(`{"recipe_path":"recipe.json"}`))
			if (err != nil) != (scenario.outcome == "") {
				t.Fatal(out, err)
			}
			if httpCalls != 1 {
				t.Fatal("unexpected HTTP retries", httpCalls)
			}
			if scenario.outcome == "" {
				if _, err := db.Entry("feed_state", "moltbook"); err == nil {
					t.Fatal("failed run advanced feed state")
				}
				return
			}
			entry, err := db.Entry("feed_state", "moltbook")
			if err != nil || entry.Revision != 1 {
				t.Fatal(entry, err)
			}
			var state struct {
				Seen    []string `json:"seen_ids"`
				Outcome string   `json:"outcome"`
				Retry   string   `json:"retry_after"`
				Status  int      `json:"last_http_status"`
			}
			if err := json.Unmarshal(entry.Data, &state); err != nil {
				t.Fatal(err)
			}
			if state.Outcome != scenario.outcome || state.Status != scenario.status || client.calls != scenario.drafts {
				t.Fatal(string(entry.Data), client.calls)
			}
			if scenario.status == 429 && state.Retry != "120" {
				t.Fatal("retry header was not persisted")
			}
			run, err := db.Run(runID)
			if err != nil {
				t.Fatal(err)
			}
			saved := false
			for _, step := range run.Record.Steps {
				if step.ID == "save_draft" && step.Status == "completed" {
					saved = true
					var artifact workflow.Artifact
					if err := json.Unmarshal([]byte(step.Output), &artifact); err != nil {
						t.Fatal(err)
					}
					writer, _, err := db.EditRun(runID)
					if err != nil {
						t.Fatal(err)
					}
					_, data, err := writer.Artifacts("save_draft").Read(t.Context(), artifact.ID)
					writer.Close()
					if err != nil || !strings.Contains(string(data), `"published": false`) {
						t.Fatal(string(data), err)
					}
				}
			}
			if saved != (scenario.drafts > 0) {
				t.Fatal("unexpected saved draft", saved)
			}
			if scenario.drafts == 1 {
				if len(state.Seen) != 1 || state.Seen[0] != "post-1" {
					t.Fatal(state.Seen)
				}
				if _, err := svc.Run(t.Context(), json.RawMessage(`{"recipe_path":"recipe.json"}`)); err != nil {
					t.Fatal(err)
				}
				entry, err = db.Entry("feed_state", "moltbook")
				if err != nil || entry.Revision != 2 || client.calls != 1 {
					t.Fatal(entry, client.calls, err)
				}
				if err := json.Unmarshal(entry.Data, &state); err != nil || state.Outcome != "no_new_posts" {
					t.Fatal(string(entry.Data), err)
				}
			}
		})
	}
}
