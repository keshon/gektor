package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
)

func TestWorkflowErrorRouteAndFinalizerNeedNoModel(t *testing.T) {
	directory, base := t.TempDir(), t.TempDir()
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "workflows", "error-routing", "recipe.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(directory, "recipe.json"), source, 0600)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = executeWorkflow(t.Context(), workflowOptions{workspace: directory, recipe: "recipe.json", storeDir: base, allow: "workflow_run=*,script_transform=*,artifact_write=*"}, &out, permissionGateContext(true, nil, nil))
	if err == nil {
		t.Fatal("failure hidden by finalizer", out.String())
	}
	var summary struct {
		ID     string `json:"run_id"`
		Status string
	}
	err = json.Unmarshal(out.Bytes(), &summary)
	if err != nil || summary.Status != "failed" {
		t.Fatal(out.String(), err)
	}
	db, err := store.OpenStore(base, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	state, err := db.Run(summary.ID)
	if err != nil || state.Record.Steps[0].Failure == nil || state.Record.Steps[0].Failure.Code != "script_failed" {
		t.Fatal(state, err)
	}
	out.Reset()
	err = workflowArtifacts(t.Context(), []string{"artifacts", "-workspace", directory, summary.ID}, &out, base)
	if err != nil {
		t.Fatal(err)
	}
	var artifacts []workflow.Artifact
	err = json.Unmarshal(out.Bytes(), &artifacts)
	if err != nil || len(artifacts) != 2 {
		t.Fatal(out.String(), err)
	}
	names := map[string]bool{}
	for _, artifact := range artifacts {
		names[artifact.Name] = true
	}
	if !names["failure.txt"] || !names["finished.txt"] || names["after.txt"] {
		t.Fatal(names)
	}
}
