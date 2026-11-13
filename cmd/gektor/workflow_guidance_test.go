package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
)

func TestWorkflowReviewGuidance(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	db, err := store.OpenStore(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Save(workflow.Run{Status: "waiting_review", Definition: workflow.Definition{Steps: []workflow.Step{{ID: "save", Review: true}}}, Steps: []workflow.StepResult{{ID: "save", Status: "waiting_review"}}}); err != nil {
		t.Fatal(err)
	}
	id := w.ID()
	w.Close()
	result, _ := json.Marshal(map[string]string{"run_id": id, "status": "waiting_review"})
	var note bytes.Buffer
	workflowGuidance(&note, string(result), home, dir, "workflow_run=*,record_write=reports/*", "http_request=*DELETE*Copy")
	for _, want := range []string{"workflow inspect", "-decision approve", "-decision reject", `-workspace "` + dir + `"`, `"` + id + `" "save"`, `-allow "workflow_run=*,record_write=reports/*"`, `-deny "http_request=*DELETE*Copy"`} {
		if !strings.Contains(note.String(), want) {
			t.Fatalf("missing %q in %s", want, &note)
		}
	}
	note.Reset()
	workflowGuidance(&note, string(result), home, dir, "workflow_run=$(unsafe)", "")
	if strings.Contains(note.String(), "$(unsafe)") || strings.Contains(note.String(), "Approve:") {
		t.Fatal("unsafe command emitted")
	}
	note.Reset()
	workflowGuidance(&note, `{"status":"completed"}`, home, dir, "", "")
	if note.Len() != 0 {
		t.Fatal("completed run emitted review guidance")
	}
}

func TestWorkflowMissingRunErrors(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	for _, command := range [][]string{
		{"inspect", "-workspace", dir, "fetch"},
		{"review", "-workspace", dir, "-decision", "approve", "fetch", "save"},
	} {
		var out bytes.Buffer
		var err error
		if command[0] == "inspect" {
			err = workflowHistory(t.Context(), command, &out, home)
		} else {
			err = workflowQueue(t.Context(), command, &out, home)
		}
		if err == nil || !strings.Contains(err.Error(), `run "fetch" not found`) || !strings.Contains(err.Error(), "workflow runs") {
			t.Fatalf("%v: %v", command, err)
		}
	}
}
