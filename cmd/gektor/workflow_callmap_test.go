package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
)

const mappedRoot = `{"version":4,"name":"Mapped writer","steps":[{"id":"batch","tool":"workflow_call","review":true,"args":{"recipe_path":"child.json"},"for_each":[{"text":"same"},{"text":"same"},{"text":"same"}]}],"outputs":{"batch":{"$step":"batch"}}}`

func TestMappedCallReviewsPreserveCompletedItems(t *testing.T) {
	for _, tc := range []struct{ reject, nested bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		t.Run(fmt.Sprintf("reject=%t/nested=%t", tc.reject, tc.nested), func(t *testing.T) {
			reject := tc.reject
			dir, home := compositionFiles(t)
			leafID := func(n int) string { return fmt.Sprintf("batch[%d]/save", n) }
			if tc.nested {
				leafID = func(n int) string { return fmt.Sprintf("batch[%d]/inner[0]/save", n) }
				middle := `{"version":4,"name":"Middle map","inputs":{"text":{"type":"string","required":true}},"steps":[{"id":"inner","tool":"workflow_call","args":{"recipe_path":"leaf.json"},"for_each":[{"text":{"$input":"text"}}]}],"outputs":{"artifact":{"$step":"inner","pointer":"/items/0/output/artifact"}}}`
				writeErr := os.WriteFile(filepath.Join(dir, "leaf.json"), []byte(compositionChild), 0600)
				if writeErr != nil {
					t.Fatal(writeErr)
				}
				writeErr = os.WriteFile(filepath.Join(dir, "child.json"), []byte(middle), 0600)
				if writeErr != nil {
					t.Fatal(writeErr)
				}
			}
			err := os.WriteFile(filepath.Join(dir, "root.json"), []byte(mappedRoot), 0600)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "root.json", storeDir: home}, &out, permissionGateContext(true, nil, nil))
			if err != nil {
				t.Fatal(out.String(), err)
			}
			var result struct {
				ID string `json:"run_id"`
			}
			err = json.Unmarshal(out.Bytes(), &result)
			if err != nil {
				t.Fatal(err)
			}
			db, err := store.OpenStore(home, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			state, err := db.Run(result.ID)
			if err != nil || state.Record.Status != "waiting_review" || len(state.Record.Steps[0].Iterations) != 0 {
				t.Fatal("entry gate created items", state, err)
			}
			review := func(id, decision string) {
				t.Helper()
				out.Reset()
				err = workflowQueue(t.Context(), []string{"review", "-workspace", dir, "-decision", decision, result.ID, id}, &out, home)
				if err != nil {
					t.Fatal(id, out.String(), err)
				}
			}
			review("batch", "approve")
			state, err = db.Run(result.ID)
			if err != nil {
				t.Fatal(err)
			}
			items := state.Record.Steps[0].Iterations
			if len(items) != 3 || items[0].Status != "waiting_review" || items[0].Child == nil || items[1].Child != nil || items[1].Status != "pending" {
				t.Fatal("map pause lost dispatch state", items)
			}
			found := false
			for _, step := range service.InspectSteps(state.Record) {
				if step.ID == leafID(0) && step.Operation == "artifact_write" && step.Status == "waiting_review" {
					found = true
				}
			}
			if !found {
				t.Fatal("child review missing from inspection")
			}
			err = os.Remove(filepath.Join(dir, "child.json"))
			if err != nil {
				t.Fatal(err)
			}
			review(leafID(0), "approve")
			state, err = db.Run(result.ID)
			if err != nil {
				t.Fatal(err)
			}
			first := state.Record.Steps[0].Iterations[0]
			if first.Status != "completed" || first.Child.Finished == nil || state.Record.Steps[0].Iterations[1].Status != "waiting_review" {
				t.Fatal("first item did not stay complete", state)
			}
			if reject {
				review(leafID(1), "reject")
			} else {
				review(leafID(1), "approve")
				review(leafID(2), "approve")
			}
			w, record, err := db.EditRun(result.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			artifacts, err := w.ListArtifacts()
			if err != nil {
				t.Fatal(err)
			}
			items = record.Steps[0].Iterations
			if items[0].Output != first.Output || !items[0].Child.Finished.Equal(*first.Child.Finished) {
				t.Fatal("completed child replayed")
			}
			if reject {
				if record.Status != "cancelled" || len(artifacts) != 1 || items[0].Status != "completed" || items[1].Status != "skipped" || items[1].Child.Status != "cancelled" || items[2].Status != "skipped" {
					t.Fatal("rejection lost item states", record)
				}
				return
			}
			if record.Status != "completed" || len(artifacts) != 3 {
				t.Fatal("duplicate inputs lost identity", record.Status, artifacts)
			}
			for n, item := range items {
				var value struct{ Artifact workflow.Artifact }
				err = json.Unmarshal([]byte(item.Output), &value)
				if err != nil {
					t.Fatal(err)
				}
				_, payload, readErr := w.Artifacts("").Read(t.Context(), value.Artifact.ID)
				if readErr != nil || string(payload) != "same" || value.Artifact.StepID != leafID(n) {
					t.Fatal("item artifact lost scope", value.Artifact, readErr)
				}
			}
		})
	}
}

func TestMappedCallValidationRejectsUnsafeOrAmbiguousRecipes(t *testing.T) {
	for _, tc := range []struct{ name, change, replacement string }{
		{"concurrent", `"review":true`, `"concurrency":2`},
		{"retry", `"review":true`, `"retry":{"max_attempts":1}`},
		{"static inputs", `"recipe_path":"child.json"`, `"recipe_path":"child.json","inputs":{"text":"static"}`},
		{"wrong input type", `{"text":"same"}`, `{"text":42}`},
		{"null item", `{"text":"same"}`, `null`},
		{"unknown input", `{"text":"same"}`, `{"typo":"same"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, home := compositionFiles(t)
			root := strings.Replace(mappedRoot, tc.change, tc.replacement, 1)
			err := os.WriteFile(filepath.Join(dir, "root.json"), []byte(root), 0600)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "root.json", storeDir: home, validateOnly: true}, &out, permissionGateContext(true, nil, nil))
			if err == nil {
				t.Fatal("validated unsafe map", out.String())
			}
		})
	}
}

func TestMappedSubworkflowExampleNeedsNoModel(t *testing.T) {
	dir, home := compositionFiles(t)
	for _, name := range []string{"recipe.json", "double.json"} {
		source, err := os.ReadFile(filepath.Join("..", "..", "examples", "workflows", "mapped-subworkflow", name))
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(filepath.Join(dir, name), source, 0600)
		if err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	err := executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "recipe.json", storeDir: home}, &out, permissionGateContext(true, nil, nil))
	if err != nil {
		t.Fatal(out.String(), err)
	}
	var result struct {
		ID      string `json:"run_id"`
		Status  string
		Outputs struct {
			Report struct {
				Completed, Failed int
				Values            []int
			}
		}
	}
	err = json.Unmarshal(out.Bytes(), &result)
	if err != nil {
		t.Fatal(err)
	}
	report := result.Outputs.Report
	if result.Status != "completed" || report.Completed != 3 || report.Failed != 1 || fmt.Sprint(report.Values) != "[4 4 6]" {
		t.Fatal(out.String())
	}
	db, err := store.OpenStore(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	state, err := db.Run(result.ID)
	if err != nil {
		t.Fatal(err)
	}
	items := state.Record.Steps[1].Iterations
	if len(items) != 4 || items[0].Child.Status != "failed" || items[1].Child.Status != "completed" || items[2].Output != items[1].Output || items[2].ID == items[1].ID {
		t.Fatal("mapped outcomes lost identity", items)
	}
}
