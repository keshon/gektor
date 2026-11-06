package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/automation"
)

func workflowWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 16, 12))); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"image.png":   b.Bytes(),
		"inputs.json": []byte(`{"source":"image.png"}`),
		"recipe.json": []byte(`{"version":2,"name":"CLI workflow","inputs":{"source":{"type":"string","required":true}},"steps":[{"id":"write","tool":"image_variants","args":{"paths":[{"$input":"source"}],"widths":[8],"out_dir":"output"}},{"id":"compare","tool":"image_compare","args":{"paths":[{"$input":"source"},{"$step":"write","pointer":"/items/0/output"}]}}]}`),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestWorkflowCLIValidatesAndRunsWithoutBackend(t *testing.T) {
	dir := workflowWorkspace(t)
	before, err := os.ReadFile(filepath.Join(dir, "image.png"))
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"validate", "run"} {
		o, err := parseWorkflowOptions([]string{action, "-workspace", dir, "-inputs", "inputs.json", "recipe.json"}, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		o.storeDir = t.TempDir()
		var out bytes.Buffer
		if err := executeWorkflow(t.Context(), o, &out, nil); err != nil {
			t.Fatal(err, out.String())
		}
		var result struct {
			ID     string `json:"run_id"`
			Valid  bool   `json:"valid"`
			Status string `json:"status"`
			Path   string `json:"record_path"`
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if action == "validate" {
			if !result.Valid {
				t.Fatal(out.String())
			}
			if _, err := os.Stat(filepath.Join(dir, ".gektor")); !os.IsNotExist(err) {
				t.Fatal("validation wrote execution state")
			}
		} else {
			if result.Status != "completed" {
				t.Fatal(out.String())
			}
			if _, err := os.Stat(result.Path); err != nil {
				t.Fatal(err)
			}
			out.Reset()
			if err := workflowHistory(t.Context(), []string{"runs", "-workspace", dir}, &out, o.storeDir); err != nil || !strings.Contains(out.String(), result.ID) {
				t.Fatal(err, out.String())
			}
			out.Reset()
			if err := workflowHistory(t.Context(), []string{"inspect", "-workspace", dir, result.ID}, &out, o.storeDir); err != nil || !strings.Contains(out.String(), `"status":"completed"`) {
				t.Fatal(err, out.String())
			}
			if _, err := os.Stat(filepath.Join(dir, "output", "image-8w.png")); err != nil {
				t.Fatal(err)
			}
			out.Reset()
			if err := executeWorkflow(t.Context(), o, &out, nil); err == nil || !strings.Contains(out.String(), `"status":"failed"`) {
				t.Fatal("existing output was reused", err, out.String())
			}
		}
	}
	after, err := os.ReadFile(filepath.Join(dir, "image.png"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("source changed", err)
	}
}

func TestWorkflowCLIRegistersSchemasAndRequiresDurableStorage(t *testing.T) {
	dir := workflowWorkspace(t)
	base := t.TempDir()
	schema := `{"type":"object","properties":{"width":{"type":"integer"}},"required":["width"]}`
	if err := os.WriteFile(filepath.Join(dir, "assets.json"), []byte(schema), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := workflowRegisterSchema(t.Context(), []string{"-workspace", dir, "assets", "assets.json"}, &out, base); err != nil {
		t.Fatal(err)
	}
	store, err := automation.OpenStore(base, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.RecordSchema("assets"); err != nil {
		t.Fatal(err)
	}
	if err := workflowRegisterSchema(t.Context(), []string{"-workspace", dir, "assets", "../outside.json"}, io.Discard, base); err == nil {
		t.Fatal("read outside workspace")
	}
	broken := filepath.Join(base, "file")
	if err := os.WriteFile(broken, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	o := workflowOptions{workspace: dir, recipe: "recipe.json", inputs: "inputs.json", storeDir: broken}
	if err := executeWorkflow(t.Context(), o, io.Discard, nil); err == nil {
		t.Fatal("ran without durable storage")
	}
	if _, err := os.Stat(filepath.Join(dir, "output")); !os.IsNotExist(err) {
		t.Fatal("storage failure had effects", err)
	}
}

func TestWorkflowCLIPermissions(t *testing.T) {
	for _, tc := range []struct {
		name, deny   string
		earlierWrite bool
	}{
		{"outer", "workflow_run=*", false},
		{"inputs", "read_file=inputs.json", false},
		{"generated filename", "image_variants=output/image-8w.png", false},
		{"resolved path", "image_compare=output/image-8w.png", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := workflowWorkspace(t)
			o, err := parseWorkflowOptions([]string{"run", "-workspace", dir, "-inputs", "inputs.json", "-deny", tc.deny, "recipe.json"}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			o.storeDir = t.TempDir()
			var out bytes.Buffer
			if err := executeWorkflow(t.Context(), o, &out, nil); err == nil {
				t.Fatal("permission bypass", out.String())
			}
			_, err = os.Stat(filepath.Join(dir, "output", "image-8w.png"))
			if tc.earlierWrite && err != nil {
				t.Fatal("earlier authorized step did not finish", err)
			}
			if !tc.earlierWrite && !os.IsNotExist(err) {
				t.Fatal("denied write created output", err)
			}
		})
	}
}

func TestWorkflowCLIRejectsInvalidInputFiles(t *testing.T) {
	for _, body := range []string{"null", "[]", `{} {}`, `{"unexpected":1}`, `{"source":true}`, strings.Repeat(" ", 64<<10) + "{}"} {
		dir := workflowWorkspace(t)
		if err := os.WriteFile(filepath.Join(dir, "inputs.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		o := workflowOptions{workspace: dir, recipe: "recipe.json", inputs: "inputs.json"}
		if err := executeWorkflow(t.Context(), o, io.Discard, nil); err == nil {
			t.Fatalf("accepted inputs %.40q", body)
		}
		if _, err := os.Stat(filepath.Join(dir, ".gektor")); !os.IsNotExist(err) {
			t.Fatal("invalid inputs created a run")
		}
	}
	dir := workflowWorkspace(t)
	for _, path := range []string{"../inputs.json", "credentials.json"} {
		o := workflowOptions{workspace: dir, recipe: "recipe.json", inputs: path}
		if err := executeWorkflow(t.Context(), o, io.Discard, nil); err == nil {
			t.Fatal("read escaped permission/root checks")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := executeWorkflow(ctx, workflowOptions{workspace: dir, recipe: "recipe.json"}, io.Discard, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestWorkflowCommandParsing(t *testing.T) {
	for _, args := range [][]string{{}, {"unknown"}, {"run"}, {"run", "-run-timeout=-1s", "recipe.json"}, {"run", "recipe.json", "extra"}, {"run", "-backend", "http://localhost", "recipe.json"}} {
		if _, err := parseWorkflowOptions(args, io.Discard); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
	for _, args := range [][]string{{}, {"recipe.json"}, {"recipe.json", "-tui", "task"}, {"recipe.json", "-resume", "old", "task"}, {"recipe.json", "-mcp", "host=command", "task"}, {"recipe.json", "-context-report", "task"}} {
		if _, err := workflowDraftOptions(args); err == nil {
			t.Fatalf("accepted draft flags %q", args)
		}
	}
	o, err := workflowDraftOptions([]string{"recipe.json", "-workspace", "project", "-backend-kind", "llama", "prepare images"})
	if err != nil || o.workflowRecipe != "recipe.json" || o.workspace != "project" || o.task != "prepare images" {
		t.Fatal(o, err)
	}
}
