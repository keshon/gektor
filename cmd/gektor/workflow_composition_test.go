package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

const compositionChild = `{"version":4,"name":"Reusable writer","inputs":{"text":{"type":"string","required":true}},"steps":[{"id":"save","tool":"artifact_write","review":true,"args":{"name":"note.txt","text":{"$input":"text"}}}],"outputs":{"artifact":{"$step":"save"}}}`
const compositionRoot = `{"version":4,"name":"Parent","steps":[{"id":"first","tool":"workflow_call","args":{"recipe_path":"child.json","inputs":{"text":"first text"}}},{"id":"second","tool":"workflow_call","args":{"recipe_path":"child.json","inputs":{"text":{"$step":"first","pointer":"/artifact/name"}}}}],"outputs":{"first":{"$step":"first","pointer":"/artifact"},"second":{"$step":"second","pointer":"/artifact"}}}`

func TestCompositionCollectsOutputsWhenAllEffectsSkipped(t *testing.T) {
	for _, child := range []bool{false, true} {
		for _, missing := range []bool{false, true} {
			t.Run(fmt.Sprintf("child=%t/missing=%t", child, missing), func(t *testing.T) {
				dir, home := t.TempDir(), t.TempDir()
				outputs := `{"done":true,"echo":{"$input":"text"}}`
				if missing {
					outputs = `{"missing":{"$step":"save"}}`
				}
				source := `{"version":4,"name":"Skipped effects","inputs":{"text":{"type":"string","default":"echo"}},"steps":[{"id":"save","tool":"artifact_write","when":false,"args":{"name":"note.txt","text":"never"}}],"outputs":` + outputs + `}`
				if err := os.WriteFile(filepath.Join(dir, "child.json"), []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
				path := "child.json"
				if child {
					path = "root.json"
					root := `{"version":4,"name":"Caller","steps":[{"id":"call","tool":"workflow_call","args":{"recipe_path":"child.json"}}],"outputs":{"result":{"$step":"call"}}}`
					if err := os.WriteFile(filepath.Join(dir, path), []byte(root), 0600); err != nil {
						t.Fatal(err)
					}
				}
				var out bytes.Buffer
				err := executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: path, storeDir: home}, &out, permissionGateContext(true, nil, nil))
				if missing {
					if err == nil {
						t.Fatal("missing output was silently omitted", out.String())
					}
					return
				}
				if err != nil {
					t.Fatal(err, out.String())
				}
				var result struct{ Outputs map[string]json.RawMessage }
				if err := json.Unmarshal(out.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				values := result.Outputs
				if child {
					if err := json.Unmarshal(values["result"], &values); err != nil {
						t.Fatal(err)
					}
				}
				if string(values["done"]) != "true" || string(values["echo"]) != `"echo"` {
					t.Fatal(out.String())
				}
			})
		}
	}
}

func compositionFiles(t *testing.T) (string, string) {
	t.Helper()
	dir, home := t.TempDir(), t.TempDir()
	for name, data := range map[string]string{"root.json": compositionRoot, "child.json": compositionChild} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, home
}

func TestSubworkflowReviewsOutputsAndFrozenRecipes(t *testing.T) {
	dir, home := compositionFiles(t)
	var out bytes.Buffer
	err := executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "root.json", storeDir: home}, &out, permissionGateContext(true, nil, nil))
	if err != nil {
		t.Fatal(out.String(), err)
	}
	var run struct {
		ID     string `json:"run_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out.Bytes(), &run); err != nil || run.Status != "waiting_review" {
		t.Fatal(out.String(), err)
	}
	if err := os.WriteFile(filepath.Join(dir, "child.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"first/save", "second/save"} {
		out.Reset()
		err := workflowQueue(t.Context(), []string{"review", "-workspace", dir, "-decision", "approve", run.ID, step}, &out, home)
		if err != nil {
			t.Fatal(step, out.String(), err)
		}
	}
	var summary struct {
		Status  string                       `json:"status"`
		Outputs map[string]workflow.Artifact `json:"outputs"`
	}
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil || summary.Status != "completed" || len(summary.Outputs) != 2 {
		t.Fatal(out.String(), err)
	}
	db, err := store.OpenStore(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w, record, err := db.EditRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if len(record.Steps) != 3 || record.Steps[0].Child == nil || record.Steps[1].Child == nil || len(record.Steps[0].Child.Steps) != 2 || record.Steps[0].Child.Steps[0].ID != "save" {
		t.Fatal("child calls were flattened instead of retaining scopes", record)
	}
	items, err := w.ListArtifacts()
	if err != nil || len(items) != 2 {
		t.Fatal("completed child replayed", items, err)
	}
	for key, want := range map[string]string{"first": "first text", "second": "note.txt"} {
		a := summary.Outputs[key]
		_, data, err := w.Artifacts("").Read(t.Context(), a.ID)
		if err != nil || string(data) != want || a.StepID != key+"/save" {
			t.Fatal(a, string(data), err)
		}
	}
}

func TestQueuedSubworkflowSnapshotsChildren(t *testing.T) {
	dir, home := compositionFiles(t)
	var out bytes.Buffer
	if err := workflowQueue(t.Context(), []string{"submit", "-workspace", dir, "root.json"}, &out, home); err != nil {
		t.Fatal(err)
	}
	var job store.Job
	if err := json.Unmarshal(out.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(job.Source, []byte("Reusable writer")) {
		t.Fatal("child not frozen")
	}
	if err := os.Remove(filepath.Join(dir, "child.json")); err != nil {
		t.Fatal(err)
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
	claimed, ok, err := db.Claim(t.Context())
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := svc.ExecuteJob(t.Context(), db, claimed); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.Jobs(t.Context())
	if err != nil || jobs[0].Status != "waiting_review" {
		t.Fatal(jobs, err)
	}
}

func TestSubworkflowRejectsCycleTypesAndForbiddenCalls(t *testing.T) {
	for _, tc := range []struct{ name, root, child, want string }{
		{"cycle", compositionRoot, strings.Replace(compositionRoot, "child.json", "root.json", -1), "cycle"},
		{"type", strings.Replace(compositionRoot, `"text":"first text"`, `"text":42`, 1), compositionChild, "string"},
		{"foreach", strings.Replace(compositionRoot, `"id":"first"`, `"id":"first","for_each":[]`, 1), compositionChild, "for_each"},
		{"outputs", compositionRoot, `{"version":4,"name":"No outputs","steps":[{"id":"save","tool":"artifact_write","args":{"name":"x","text":"x"}}]}`, "declare outputs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, home := compositionFiles(t)
			os.WriteFile(filepath.Join(dir, "root.json"), []byte(tc.root), 0600)
			os.WriteFile(filepath.Join(dir, "child.json"), []byte(tc.child), 0600)
			var out bytes.Buffer
			err := executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "root.json", storeDir: home, validateOnly: true}, &out, permissionGateContext(true, nil, nil))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal(out.String(), err)
			}
		})
	}
}

func TestSubworkflowBranchGatesAndMergeAny(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			dir, home := compositionFiles(t)
			child := `{"version":4,"name":"Branches","inputs":{"text":{"type":"string","default":"never written"}},"steps":[{"id":"skip","tool":"artifact_write","when":false,"args":{"name":"skip","text":"skip"}},{"id":"merge","tool":"artifact_write","merge":"any","depends_on":["skip"],"args":{"name":"merge","text":{"$input":"text"}}}],"outputs":{"done":true}}`
			root := fmt.Sprintf(`{"version":4,"name":"Gate","steps":[{"id":"call","tool":"workflow_call","when":%t,"args":{"recipe_path":"child.json"}}]}`, enabled)
			os.WriteFile(filepath.Join(dir, "child.json"), []byte(child), 0600)
			os.WriteFile(filepath.Join(dir, "root.json"), []byte(root), 0600)
			var out bytes.Buffer
			err := executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "root.json", storeDir: home}, &out, permissionGateContext(true, nil, nil))
			if err != nil {
				t.Fatal(out.String(), err)
			}
			var run struct {
				ID string `json:"run_id"`
			}
			json.Unmarshal(out.Bytes(), &run)
			db, err := store.OpenStore(home, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			w, _, err := db.EditRun(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			items, err := w.ListArtifacts()
			if err != nil || len(items) != 0 {
				t.Fatal("binding incorrectly enabled a branch", items, err)
			}
		})
	}
}

func TestSubworkflowPermissionAndNestingBounds(t *testing.T) {
	dir, home := compositionFiles(t)
	var out bytes.Buffer
	err := executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "root.json", storeDir: home, deny: "workflow_call=child.json"}, &out, permissionGateContext(true, nil, nil))
	if err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Fatal(out.String(), err)
	}
	for i := 0; i < 6; i++ {
		source := fmt.Sprintf(`{"version":4,"name":"Nested","steps":[{"id":"call","tool":"workflow_call","args":{"recipe_path":"level%d.json"}}],"outputs":{"value":{"$step":"call"}}}`, i+1)
		if i == 5 {
			source = strings.ReplaceAll(compositionChild, `"review":true,`, "")
		}
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("level%d.json", i)), []byte(source), 0600)
	}
	out.Reset()
	err = executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "level0.json", storeDir: home, validateOnly: true}, &out, permissionGateContext(true, nil, nil))
	if err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatal(out.String(), err)
	}
}

func TestNestedSubworkflowInputAndOutputScopes(t *testing.T) {
	dir, home := compositionFiles(t)
	leaf := compositionChild
	middle := `{"version":4,"name":"Middle","inputs":{"text":{"type":"string","required":true}},"steps":[{"id":"inner","tool":"workflow_call","args":{"recipe_path":"leaf.json","inputs":{"text":{"$input":"text"}}}}],"outputs":{"artifact":{"$step":"inner","pointer":"/artifact"}}}`
	os.WriteFile(filepath.Join(dir, "child.json"), []byte(middle), 0600)
	os.WriteFile(filepath.Join(dir, "leaf.json"), []byte(leaf), 0600)
	var out bytes.Buffer
	err := executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "root.json", storeDir: home}, &out, permissionGateContext(true, nil, nil))
	if err != nil {
		t.Fatal(out.String(), err)
	}
	var run struct {
		ID string `json:"run_id"`
	}
	err = json.Unmarshal(out.Bytes(), &run)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"first/inner/save", "second/inner/save"} {
		out.Reset()
		err = workflowQueue(t.Context(), []string{"review", "-workspace", dir, "-decision", "approve", run.ID, step}, &out, home)
		if err != nil {
			t.Fatal(step, out.String(), err)
		}
	}
	var result struct {
		Status  string                       `json:"status"`
		Outputs map[string]workflow.Artifact `json:"outputs"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Status != "completed" || result.Outputs["first"].StepID != "first/inner/save" || result.Outputs["second"].StepID != "second/inner/save" {
		t.Fatal(out.String(), err)
	}
}

func TestCallEntryReviewAndNestedRejection(t *testing.T) {
	dir, home := compositionFiles(t)
	root := strings.Replace(compositionRoot, `"id":"first"`, `"id":"first","review":true`, 1)
	err := os.WriteFile(filepath.Join(dir, "root.json"), []byte(root), 0600)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "root.json", storeDir: home}, &out, permissionGateContext(true, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	var run struct {
		ID string `json:"run_id"`
	}
	err = json.Unmarshal(out.Bytes(), &run)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.OpenStore(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	state, err := db.Run(run.ID)
	if err != nil || state.Record.Steps[0].Status != "waiting_review" || state.Record.Steps[0].Child != nil {
		t.Fatal("entry review instantiated the child", state, err)
	}
	out.Reset()
	err = workflowQueue(t.Context(), []string{"review", "-workspace", dir, "-decision", "approve", run.ID, "first"}, &out, home)
	if err != nil {
		t.Fatal(err)
	}
	state, err = db.Run(run.ID)
	if err != nil || state.Record.Steps[0].Child == nil || state.Record.Steps[0].Child.Status != "waiting_review" || state.Record.Steps[0].Status != "waiting_review" {
		t.Fatal("child review did not pause the call", state, err)
	}
	out.Reset()
	err = workflowQueue(t.Context(), []string{"review", "-workspace", dir, "-decision", "approve", run.ID, "first"}, &out, home)
	if err == nil {
		t.Fatal("offered an already approved call entry instead of its child review")
	}
	out.Reset()
	err = workflowQueue(t.Context(), []string{"review", "-workspace", dir, "-decision", "reject", run.ID, "first/save"}, &out, home)
	if err != nil {
		t.Fatal(err)
	}
	state, err = db.Run(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	child := state.Record.Steps[0].Child
	if state.Record.Status != "cancelled" || state.Record.Steps[0].Status != "skipped" || child.Status != "cancelled" || child.Finished == nil || child.Steps[0].Status != "skipped" || state.Record.Steps[1].Status != "skipped" {
		t.Fatal("rejection left an active child scope", state)
	}
}

func TestChildContinuationRechecksCallPermission(t *testing.T) {
	dir, home := compositionFiles(t)
	var out bytes.Buffer
	err := executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "root.json", storeDir: home}, &out, permissionGateContext(true, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	var run struct {
		ID string `json:"run_id"`
	}
	err = json.Unmarshal(out.Bytes(), &run)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	svc, err := app.NewWorkflow(ws, home, buildPolicy("", "workflow_call=child.json"), permissionGateContext(true, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Review(t.Context(), run.ID, "first/save", true)
	if err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Fatal("continued through revoked call permission", err)
	}
	db, err := store.OpenStore(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w, _, err := db.EditRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	artifacts, err := w.ListArtifacts()
	if err != nil || len(artifacts) != 0 {
		t.Fatal("child effect escaped revoked permission", artifacts, err)
	}
}

func TestChildScopeRunsCollectionReportWithoutModel(t *testing.T) {
	dir, home := compositionFiles(t)
	child, err := os.ReadFile(filepath.Join("..", "..", "examples", "workflows", "collection-report", "recipe.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "child.json"), child, 0600)
	if err != nil {
		t.Fatal(err)
	}
	root := `{"version":4,"name":"Caller","steps":[{"id":"call","tool":"workflow_call","args":{"recipe_path":"child.json","inputs":{"numbers":[-1,2,2,3]}}}],"outputs":{"report":{"$step":"call","pointer":"/report"}}}`
	err = os.WriteFile(filepath.Join(dir, "root.json"), []byte(root), 0600)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "root.json", storeDir: home}, &out, permissionGateContext(true, nil, nil))
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
		t.Fatal("child collection lost outcomes or duplicate inputs", out.String())
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
	items := state.Record.Steps[0].Child.Steps[1].Iterations
	if len(items) != 4 || items[0].Status != "failed" || items[1].Output != "4" || items[2].Output != "4" || items[3].Output != "6" || items[0].ID != "call/double[0]" {
		t.Fatal("child item checkpoints lost namespace or ordering", items)
	}
}
