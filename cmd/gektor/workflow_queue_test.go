package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/workflow/store"
)

func TestWorkflowQueueSnapshotsAndCommands(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	source := []byte(`{"version":3,"name":"snapshot","steps":[{"id":"js","tool":"script_transform","args":{"code":"return input;","input":42}}]}`)
	if err := os.WriteFile(filepath.Join(dir, "recipe.json"), source, 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := workflowQueue(t.Context(), append([]string{args[0], "-workspace", dir}, args[1:]...), &out, home)
		return out.String(), err
	}
	out, err := invoke("submit", "recipe.json")
	if err != nil {
		t.Fatal(err)
	}
	var j store.Job
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "recipe.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, err = invoke("job", j.ID)
	if err != nil {
		t.Fatal(err)
	}
	var saved store.Job
	if err := json.Unmarshal([]byte(out), &saved); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved.Source, source) {
		t.Fatalf("snapshot changed: %s", saved.Source)
	}
	if _, err := invoke("cancel", j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke("serve", "extra"); err == nil {
		t.Fatal("extra arguments ignored")
	}
	if _, err := invoke("review", "run", "step"); err == nil {
		t.Fatal("missing decision accepted")
	}
}
