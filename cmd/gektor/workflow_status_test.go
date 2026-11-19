package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/keshon/gektor/internal/workflow/store"
)

func TestWorkflowStatusUsesStoredMetadataWithoutStartingWorker(t *testing.T) {
	workspace, directory := t.TempDir(), t.TempDir()
	db, err := store.OpenStore(directory, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Enqueue(t.Context(), "absent.json", json.RawMessage(`{"version":4}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = workflowQueue(t.Context(), []string{"status", "-workspace", workspace}, &out, directory)
	if err != nil {
		t.Fatal(err)
	}
	var status store.Status
	err = json.Unmarshal(out.Bytes(), &status)
	if err != nil || status.Jobs["queued"] != 1 || status.OldestQueued == nil || status.WorkerLeaseHeld || status.LastWorkerTick != nil {
		t.Fatal(status, err)
	}
	job, claimed, err := db.Claim(t.Context())
	if err != nil || !claimed || job.RunID != "" {
		t.Fatal("observation executed or consumed queued work", job, claimed, err)
	}
}
