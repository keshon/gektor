package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/tools/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

func TestWorkflowVerifierAcceptsRealRunsAndRejectsTampering(t *testing.T) {
	for _, kind := range []string{"workflow_success", "workflow_icc", "workflow_invalid", "workflow_existing"} {
		t.Run(kind, func(t *testing.T) {
			files, err := workflowFixture(kind)
			if err != nil {
				t.Fatal(err)
			}
			again, _ := workflowFixture(kind)
			if !bytes.Equal(files["image.png"], again["image.png"]) {
				t.Fatal("fixture not deterministic")
			}
			dir := t.TempDir()
			for path, data := range files {
				target := filepath.Join(dir, filepath.FromSlash(path))
				if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			ws, err := workspace.New(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			if kind == "workflow_success" || kind == "workflow_icc" {
				source := `{"version":1,"name":"Image workflow","steps":[{"id":"inspect","tool":"image_inspect","args":{"paths":["image.png"]}},{"id":"preview","tool":"image_preview","args":{"paths":["image.png"],"widths":[320,800]}},{"id":"write","tool":"image_variants","args":{"paths":["image.png"],"widths":[320,800],"out_dir":"output"}}]}`
				if kind == "workflow_success" {
					source = source[:len(source)-2] + `,{"id":"compare","tool":"image_compare","args":{"paths":["image.png","output/image-800w.png"]}}]}`
				}
				if err := ws.WriteFile("recipe.json", []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
			}
			allow := func(context.Context, string, json.RawMessage) error { return nil }
			tool := workflow.Tool{WS: ws, ValidateOnly: kind == "workflow_invalid", Authorize: allow, AuthorizeOutput: allow}
			output, runErr := tool.Run(t.Context(), json.RawMessage(`{"recipe_path":"recipe.json"}`))
			if (runErr == nil) != (kind == "workflow_success") {
				t.Fatalf("unexpected operation result: %v", runErr)
			}
			if err := checkWorkflow(kind, ws); err != nil {
				t.Fatal(err)
			}
			if kind != "workflow_invalid" {
				calls := []call{{tool: "workflow_run", args: `{"recipe_path":"recipe.json"}`, output: output}}
				if err := checkWorkflowTrace(calls, ws); err != nil {
					t.Fatal(err)
				}
				if kind == "workflow_success" {
					var result struct {
						Path string `json:"record_path"`
					}
					if err := json.Unmarshal([]byte(output), &result); err != nil {
						t.Fatal(err)
					}
					data, err := ws.ReadFile(result.Path)
					if err != nil {
						t.Fatal(err)
					}
					var r automation.Record
					if err := json.Unmarshal(data, &r); err != nil {
						t.Fatal(err)
					}
					r.Definition.Name = "forged name"
					source, err := json.Marshal(r.Definition)
					if err != nil {
						t.Fatal(err)
					}
					r.RecipeSource = string(source)
					r.RecipeSHA256 = fmt.Sprintf("%x", sha256.Sum256(source))
					r.Digest = r.RecipeSHA256
					if err := ws.WriteFile("recipe.json", source, 0600); err != nil {
						t.Fatal(err)
					}
					data, err = json.Marshal(r)
					if err != nil {
						t.Fatal(err)
					}
					if err := ws.WriteFile(result.Path, data, 0600); err != nil {
						t.Fatal(err)
					}
					if err := checkWorkflow(kind, ws); err != nil {
						t.Fatalf("forged disk evidence should be internally consistent: %v", err)
					}
					if err := checkWorkflowTrace(calls, ws); err == nil {
						t.Fatal("forged provenance passed captured result check")
					}
				}
			}
			if err := ws.WriteFile("image.png", []byte("tampered"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := checkWorkflow(kind, ws); err == nil {
				t.Fatal("accepted changed original")
			}
		})
	}
}

func TestGeneratedWorkflowFingerprintAndSuite(t *testing.T) {
	a, err := inputsDigest([]Probe{{Name: "test", Generated: "workflow_success"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := inputsDigest([]Probe{{Name: "test", Generated: "workflow_icc"}})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("generated inputs absent from fingerprint")
	}
	if _, err := workflowFixture("typo"); err == nil {
		t.Fatal("unknown generator accepted")
	}
	var probes []Probe
	for _, name := range []string{"26-workflow-success", "27-workflow-icc", "28-workflow-invalid", "29-workflow-existing"} {
		probes = append(probes, Probe{Name: name})
	}
	if selected, err := selectSuite(probes, "workflows"); err != nil || len(selected) != 4 {
		t.Fatal(selected, err)
	}
}
