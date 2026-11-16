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

func TestWorkflowRecoverUsesVerifiedOutputWithoutRepeatingArtifactWrite(t *testing.T) {
	directory, base := t.TempDir(), t.TempDir()
	source := `{"version":4,"name":"Recover artifact","steps":[{"id":"save","tool":"artifact_write","args":{"name":"note.txt","text":"verified"}}],"outputs":{"saved":{"$step":"save"}}}`
	err := os.WriteFile(filepath.Join(directory, "root.json"), []byte(source), 0600)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = executeWorkflow(t.Context(), workflowOptions{workspace: directory, recipe: "root.json", storeDir: base}, &out, permissionGateContext(true, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		ID string `json:"run_id"`
	}
	err = json.Unmarshal(out.Bytes(), &summary)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.OpenStore(base, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	writer, r, err := db.EditRun(summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	verified := json.RawMessage(r.Steps[0].Output)
	r.Status, r.Finished = "running", nil
	r.Steps[0].Status, r.Steps[0].Output = "running", ""
	r.Steps[1].Status, r.Steps[1].Output = "pending", ""
	err = writer.Save(r)
	closeErr := writer.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	request := workflow.Recovery{Note: "Verified the published artifact", Decisions: []workflow.RecoveryDecision{{StepID: "save", Action: "complete", Output: verified}}}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(directory, "recovery.json"), raw, 0600)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err = workflowQueue(t.Context(), []string{"recover", "-workspace", directory, "-recovery", "recovery.json", summary.ID}, &out, base)
	if err != nil {
		t.Fatal(out.String(), err)
	}
	state, err := db.Run(summary.ID)
	if err != nil || state.Status != "completed" || len(state.Record.Recoveries) != 1 || state.Record.Recoveries[0].Decisions[0].OutputSHA256 == "" {
		t.Fatal(state, err)
	}
	if state.Record.Steps[0].Output != string(verified) || len(state.Record.Steps[0].Attempts) != 1 {
		t.Fatal("artifact write repeated", state.Record.Steps[0])
	}
}
