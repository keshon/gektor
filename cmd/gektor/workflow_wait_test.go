package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestDurableWaitExampleThroughCLIAndWorkerContinuation(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	body, err := os.ReadFile(filepath.Join("../..", "examples/workflows/durable-wait/recipe.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "recipe.json"), body, 0600)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "recipe.json", storeDir: home}, &out, permissionGateContext(true, nil, nil))
	if err != nil {
		t.Fatal(out.String(), err)
	}
	var summary struct {
		ID       string    `json:"run_id"`
		Status   string    `json:"status"`
		ResumeAt time.Time `json:"resume_at"`
	}
	err = json.Unmarshal(out.Bytes(), &summary)
	if err != nil || summary.Status != "waiting" || summary.ResumeAt.IsZero() {
		t.Fatal(out.String(), err)
	}
	var note bytes.Buffer
	workflowGuidance(&note, out.String(), home, dir, "", "")
	if !strings.Contains(note.String(), "workflow service") || strings.Contains(note.String(), "Approve:") {
		t.Fatal(note.String())
	}

	ws, err := workspace.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	svc, err := app.NewWorkflow(ws, home, buildPolicy("", ""), permissionGateContext(true, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.OpenStore(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	before, err := db.Run(summary.ID)
	if err != nil || before.Status != "waiting" || before.Record.Steps[0].Status != "completed" || before.Record.Steps[2].Status != "pending" {
		t.Fatal(before, err)
	}

	timer := time.NewTimer(time.Until(summary.ResumeAt))
	defer timer.Stop()
	select {
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	case <-timer.C:
	}
	writer, r, ok, err := db.ClaimWait(t.Context(), time.Now())
	if err != nil || !ok {
		t.Fatal("missing deadline", err)
	}
	defer writer.Close()
	err = svc.ContinueWait(t.Context(), db, writer, r)
	if err != nil {
		t.Fatal(err)
	}
	after, err := db.Run(summary.ID)
	if err != nil || after.Record.Status != "completed" {
		t.Fatal(after, err)
	}
	artifacts, err := writer.ListArtifacts()
	if err != nil || len(artifacts) != 2 {
		t.Fatal("prior effect replayed", artifacts, err)
	}
	if len(after.Record.Steps[0].Attempts) != 1 || len(after.Record.Steps[1].Attempts) != 1 {
		t.Fatal("completed work repeated", after.Record.Steps)
	}
}
