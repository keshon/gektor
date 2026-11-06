package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/permission"
)

func TestWorkflowArtifactsAcrossReviewAndExport(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	source := `{"version":3,"name":"Artifacts","steps":[{"id":"import","tool":"artifact_import","args":{"path":"source.txt"}},{"id":"read","tool":"artifact_read_text","review":true,"args":{"id":{"$step":"import","pointer":"/id"}}},{"id":"save","tool":"artifact_write","args":{"name":"report.txt","text":{"$step":"read","pointer":"/text"}}}]}`
	for name, text := range map[string]string{"recipe.json": source, "source.txt": "persistent evidence"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	allow := "workflow_run=*,artifact_import=source.txt,artifact_read_text=*,artifact_write=report.txt"
	gate := func(context.Context, string, string, json.RawMessage) (permission.Effect, error) {
		return permission.Deny, nil
	}
	var out bytes.Buffer
	err := executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "recipe.json", storeDir: home, allow: allow}, &out, gate)
	if err != nil {
		t.Fatal(out.String(), err)
	}
	var result struct {
		ID     string `json:"run_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Status != "waiting_review" {
		t.Fatal(out.String(), err)
	}
	// Original file may disappear; review must use the captured bytes.
	if err := os.Remove(filepath.Join(dir, "source.txt")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err = workflowQueue(t.Context(), []string{"review", "-workspace", dir, "-decision", "approve", "-allow", allow, result.ID, "read"}, &out, home)
	if err != nil || !strings.Contains(out.String(), `"status":"completed"`) {
		t.Fatal(out.String(), err)
	}
	out.Reset()
	err = workflowArtifacts(t.Context(), []string{"artifacts", "-workspace", dir, result.ID}, &out, home)
	if err != nil {
		t.Fatal(err)
	}
	var artifacts []automation.Artifact
	if err := json.Unmarshal(out.Bytes(), &artifacts); err != nil || len(artifacts) != 2 {
		t.Fatal(out.String(), err)
	}
	var saved automation.Artifact
	for _, a := range artifacts {
		if a.StepID == "save" {
			saved = a
		}
	}
	args := []string{"export-artifact", "-workspace", dir, result.ID, saved.ID, "export.txt"}
	out.Reset()
	if err := workflowArtifacts(t.Context(), args, &out, home); err != nil {
		t.Fatal(err)
	}
	if err := workflowArtifacts(t.Context(), args, &out, home); err == nil {
		t.Fatal("export overwrote existing file")
	}
	data, err := os.ReadFile(filepath.Join(dir, "export.txt"))
	if err != nil || string(data) != "persistent evidence" {
		t.Fatal(string(data), err)
	}
	args[len(args)-1] = "../escape.txt"
	if err := workflowArtifacts(t.Context(), args, &out, home); err == nil {
		t.Fatal("export escaped workspace")
	}
}

func TestArtifactImportDeniedBeforeRun(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	source := `{"version":3,"name":"Denied","steps":[{"id":"import","tool":"artifact_import","args":{"path":"secret.txt"}}]}`
	if err := os.WriteFile(filepath.Join(dir, "recipe.json"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "recipe.json", storeDir: home, allow: "workflow_run=*", deny: "artifact_import=*"}, &out, permissionGateContext(true, nil, nil))
	if err == nil {
		t.Fatal("denied import accepted")
	}
	db, err := store.OpenStore(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	runs, err := db.Runs()
	if err != nil || len(runs) != 0 {
		t.Fatal(runs, err)
	}
}
