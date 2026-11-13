package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowProgramHonorsDenyAndRejectsRetry(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	source := `{"version":4,"name":"Process policy","steps":[{"id":"version","tool":"run_program","args":{"program":"go","args":["version"]}}]}`
	path := filepath.Join(dir, "recipe.json")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	o := workflowOptions{workspace: dir, recipe: "recipe.json", storeDir: home, allow: "workflow_run=*", deny: "run_program=go version"}
	err := executeWorkflow(t.Context(), o, &out, permissionGateContext(true, nil, nil))
	if err == nil || !strings.Contains(err.Error(), "blocked by policy (run_program on go version)") {
		t.Fatal(out.String(), err)
	}
	source = strings.Replace(source, `"id":"version"`, `"id":"version","retry":{"max_attempts":2}`, 1)
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	o.validateOnly = true
	o.deny = ""
	out.Reset()
	err = executeWorkflow(t.Context(), o, &out, permissionGateContext(true, nil, nil))
	if err == nil || !strings.Contains(err.Error(), "does not permit retries") {
		t.Fatal(out.String(), err)
	}
}
