package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/capabilities/documents"
	workflowcore "github.com/keshon/gektor/internal/workflow"
	workflow "github.com/keshon/gektor/internal/workflow/agenttools"
	"github.com/keshon/gektor/internal/workflow/script"
	"github.com/keshon/gektor/internal/workspace"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "workflow" && os.Args[2] == "internal-document-worker" {
		if err := documents.Serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 2 && os.Args[1] == "workflow" && os.Args[2] == "internal-script-worker" {
		if err := script.Serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestPipelineCriteriaRejectEmptyAndAcceptRealOperations(t *testing.T) {
	for _, kind := range []string{"pipeline_compose", "pipeline_schedule"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "workspace")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			ws, err := workspace.New(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			if err := ws.WriteFile("double.json", []byte(doubleRecipe), 0600); err != nil {
				t.Fatal(err)
			}
			if err := checkPipeline(t.Context(), kind, ws); err == nil {
				t.Fatal("empty run passed")
			}
			allow := func(context.Context, string, json.RawMessage) error { return nil }
			if kind == "pipeline_schedule" {
				tool := workflow.Schedule{WS: ws, StoreDir: workflowStoreDir(ws), Authorize: allow, Registry: func() (*workflowcore.Registry, error) {
					return app.WorkflowRegistry(ws, workflowStoreDir(ws), nil, nil)
				},
				}
				args := json.RawMessage(`{"action":"create","id":"double-hourly","recipe_path":"double.json","cron":"0 * * * *","timezone":"UTC","inputs":{"n":7},"paused":true}`)
				if _, err := tool.Run(t.Context(), args); err != nil {
					t.Fatal(err)
				}
			} else {
				parent := `{"version":3,"name":"Two calls","steps":[{"id":"a","tool":"workflow_call","args":{"recipe_path":"double.json","inputs":{"n":3}}},{"id":"b","tool":"workflow_call","args":{"recipe_path":"double.json","inputs":{"n":5}}}],"outputs":{"six":{"$step":"a","pointer":"/value"},"ten":{"$step":"b","pointer":"/value"}}}`
				if err := ws.WriteFile("recipe.json", []byte(parent), 0600); err != nil {
					t.Fatal(err)
				}
				tool := workflow.Tool{WS: ws, StoreDir: workflowStoreDir(ws), Authorize: allow, Registry: func() (*workflowcore.Registry, error) {
					return app.WorkflowRegistry(ws, workflowStoreDir(ws), nil, nil)
				},
				}
				out, err := tool.Run(t.Context(), json.RawMessage(`{"recipe_path":"recipe.json"}`))
				if err != nil {
					t.Fatal(out, err)
				}
				if err := checkWorkflowTrace([]call{{tool: "workflow_run", args: `{"recipe_path":"recipe.json"}`, output: out}}, ws); err != nil {
					t.Fatal(err)
				}
			}
			if err := checkPipeline(t.Context(), kind, ws); err != nil {
				t.Fatal(err)
			}
			if err := ws.WriteFile("double.json", []byte(`{}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := checkPipeline(t.Context(), kind, ws); err == nil {
				t.Fatal("changed child passed")
			}
		})
	}
}

func TestVisualFixtureAndProbeInputs(t *testing.T) {
	a, err := generatedFixture("visual_attachment")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := generatedFixture("visual_attachment")
	if !bytes.Equal(a["picture.jpg"], b["picture.jpg"]) {
		t.Fatal("unstable image")
	}
	im, format, err := image.Decode(bytes.NewReader(a["picture.jpg"]))
	if err != nil || format != "png" {
		t.Fatal(format, err)
	}
	r, _, _, _ := im.At(180, 170).RGBA()
	_, _, blue, _ := im.At(680, 480).RGBA()
	if r == 0 || blue == 0 {
		t.Fatal("missing fixture shapes")
	}
	root, _ := filepath.Abs("../..")
	t.Chdir(root)
	probes, err := loadProbes(filepath.Join(root, "eval/probes"), "")
	if err != nil {
		t.Fatal(err)
	}
	for suite, count := range map[string]int{"workflows": 5, "visual": 1, "pipelines": 2, "documents": 3, "delegation": 2, "memory": 3, "media": 7, "media-decisions": 3} {
		selected, err := selectSuite(probes, suite)
		if err != nil || len(selected) != count {
			t.Fatal(suite, selected, err)
		}
	}
	probe := Probe{Name: "image", Generated: "visual_attachment", Attachments: []string{"picture.jpg"}}
	first, err := inputsDigest([]Probe{probe})
	if err != nil {
		t.Fatal(err)
	}
	probe.Attachments = nil
	second, err := inputsDigest([]Probe{probe})
	if err != nil || first == second {
		t.Fatal("delivery mode absent from fingerprint", err)
	}
}

func TestTextBackendSkipsVisualProbeWithoutModelCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/props" {
			t.Errorf("unexpected model request: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"modalities":{"vision":false}}`)
	}))
	defer server.Close()
	p := Probe{Generated: "visual_attachment", Attachments: []string{"picture.jpg"}}
	result := runOnce(t.Context(), p, 1, t.TempDir(), "llama", server.URL, "local", 8192, 1024, false, "", "", false)
	stats := aggregateRuns([]Result{result})
	if !result.Skipped || result.Errored || result.ModelCalls != 0 || stats.Skipped != 1 || stats.Errors != 0 {
		t.Fatal(result, stats)
	}
}
