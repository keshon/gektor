package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow/service"
)

func TestFixtureCLIUsesCapturedInputsAndRealScriptLogic(t *testing.T) {
	directory, normalStore, fixtureStore := t.TempDir(), t.TempDir(), t.TempDir()
	source := `{"version":4,"name":"Captured","inputs":{"count":{"type":"integer","required":true}},"steps":[{"id":"double","tool":"script_transform","args":{"input":{"$input":"count"},"code":"return input * 2"}}],"outputs":{"value":{"$step":"double"}}}`
	err := os.WriteFile(filepath.Join(directory, "recipe.json"), []byte(source), 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(directory, "inputs.json"), []byte(`{"count":3}`), 0600)
	if err != nil {
		t.Fatal(err)
	}
	options, err := parseWorkflowOptions([]string{"run", "-workspace", directory, "-inputs", "inputs.json", "-yes", "-allow", "workflow_run=*,script_transform=*", "recipe.json"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	options.storeDir = normalStore
	var output bytes.Buffer
	err = executeWorkflow(t.Context(), options, &output, nil)
	if err != nil {
		t.Fatal(output.String(), err)
	}
	var saved struct {
		ID string `json:"run_id"`
	}
	err = json.Unmarshal(output.Bytes(), &saved)
	if err != nil || saved.ID == "" {
		t.Fatal(output.String(), err)
	}
	output.Reset()
	err = workflowCapture(t.Context(), []string{"-workspace", directory, saved.ID}, &output, normalStore)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"count": 3`) || !strings.Contains(output.String(), `"calls": []`) {
		t.Fatal(output.String())
	}
	err = os.WriteFile(filepath.Join(directory, "fixture.json"), output.Bytes(), 0600)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-workspace", directory, "-fixture", "fixture.json", "-yes", "recipe.json"}
	output.Reset()
	err = workflowFixtureTest(t.Context(), args, &output, fixtureStore, nil)
	var report service.FixtureReport
	decodeErr := json.Unmarshal(output.Bytes(), &report)
	if err != nil || decodeErr != nil || !report.Passed || report.Mode != "fixture" || string(report.Outputs) != `{"value":6}` {
		t.Fatal(output.String(), err, decodeErr)
	}
	// A changed transform must fail against the captured output, not replay it.
	changed := strings.Replace(source, "input * 2", "input * 3", 1)
	err = os.WriteFile(filepath.Join(directory, "recipe.json"), []byte(changed), 0600)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	err = workflowFixtureTest(t.Context(), args, &output, fixtureStore, nil)
	if err == nil || !strings.Contains(output.String(), `"passed":false`) {
		t.Fatal("changed script silently replayed captured output", output.String(), err)
	}
	entries, err := os.ReadDir(fixtureStore)
	if err != nil || len(entries) != 0 {
		t.Fatal("test mode persisted a run", entries, err)
	}
}

func TestFixtureCLIRejectsMissingFilesAndDeniedReads(t *testing.T) {
	directory := t.TempDir()
	err := os.WriteFile(filepath.Join(directory, "fixture.json"), []byte(`{"version":1,"calls":[],"expect":{"status":"completed"}}`), 0600)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-workspace", directory, "recipe.json"},
		{"-workspace", directory, "-fixture", "fixture.json", "-run-timeout=-1s", "recipe.json"},
		{"-workspace", directory, "-fixture", "fixture.json", "-deny", "read_file=*", "-yes", "recipe.json"},
		{"-workspace", directory, "-fixture", "missing.json", "-yes", "recipe.json"},
	} {
		err = workflowFixtureTest(t.Context(), args, io.Discard, t.TempDir(), nil)
		if err == nil {
			t.Fatal("accepted invalid test invocation", args)
		}
	}
}

func TestShippedFixtureExampleRunsWithoutHTTPPermission(t *testing.T) {
	example, err := filepath.Abs(filepath.Join("..", "..", "examples", "workflows", "fixture-test"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = workflowFixtureTest(t.Context(), []string{"-workspace", example, "-fixture", "fixture.json", "-deny", "http_request=*", "-yes", "recipe.json"}, &output, t.TempDir(), nil)
	if err != nil || !strings.Contains(output.String(), `"passed":true`) || !strings.Contains(output.String(), `"value":6`) {
		t.Fatal(output.String(), err)
	}
}
