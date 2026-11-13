package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow/store"
)

func TestRepeatCallsPersistReviewsAndFrozenScopes(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprint(reject), func(t *testing.T) {
			dir, home := t.TempDir(), t.TempDir()
			root := `{"version":4,"name":"Pages","steps":[{"id":"pages","tool":"workflow_call","args":{"recipe_path":"page.json","inputs":{"cursor":0}},"repeat":{"max_iterations":3,"until":"/done","next_inputs":"/next"}}],"outputs":{"last":{"$step":"pages"}}}`
			child := `{"version":4,"name":"Page","inputs":{"cursor":{"type":"integer","required":true}},"steps":[{"id":"advance","tool":"script_transform","args":{"input":{"$input":"cursor"},"code":"return {done: input >= 2, next: {cursor: input + 1}, text: String(input)};"}},{"id":"save","tool":"artifact_write","review":true,"args":{"name":"page.txt","text":{"$step":"advance","pointer":"/text"}}}],"outputs":{"done":{"$step":"advance","pointer":"/done"},"next":{"$step":"advance","pointer":"/next"},"artifact":{"$step":"save"}}}`
			for name, source := range map[string]string{"root.json": root, "page.json": child} {
				err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600)
				if err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			err := executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: "root.json", storeDir: home}, &out, permissionGateContext(true, nil, nil))
			if err != nil {
				t.Fatal(out.String(), err)
			}
			var summary struct {
				ID     string `json:"run_id"`
				Status string `json:"status"`
			}
			err = json.Unmarshal(out.Bytes(), &summary)
			if err != nil || summary.Status != "waiting_review" {
				t.Fatal(out.String(), err)
			}
			err = os.WriteFile(filepath.Join(dir, "page.json"), []byte(`{}`), 0600)
			if err != nil {
				t.Fatal(err)
			}
			for n := 0; n < 3; n++ {
				var guidance bytes.Buffer
				workflowGuidance(&guidance, out.String(), home, dir, "", "")
				if !strings.Contains(guidance.String(), fmt.Sprintf(`"pages[%d]/save"`, n)) {
					t.Fatal("guidance selected the enclosing call instead of its pending review", guidance.String())
				}
				decision := "approve"
				if reject && n == 1 {
					decision = "reject"
				}
				out.Reset()
				err = workflowQueue(t.Context(), []string{"review", "-workspace", dir, "-decision", decision, summary.ID, fmt.Sprintf("pages[%d]/save", n)}, &out, home)
				if err != nil {
					t.Fatal(out.String(), err)
				}
				if decision == "reject" {
					break
				}
			}
			db, err := store.OpenStore(home, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			writer, record, err := db.EditRun(summary.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			wantCount, wantStatus := 3, "completed"
			if reject {
				wantCount, wantStatus = 1, "cancelled"
			}
			artifacts, err := writer.ListArtifacts()
			if err != nil || record.Status != wantStatus || len(artifacts) != wantCount {
				t.Fatalf("status=%s artifacts=%d err=%v", record.Status, len(artifacts), err)
			}
			for _, artifact := range artifacts {
				_, body, readErr := writer.Artifacts("").Read(t.Context(), artifact.ID)
				if readErr != nil {
					t.Fatal(readErr)
				}
				wantID := fmt.Sprintf("pages[%s]/save", body)
				if artifact.StepID != wantID {
					t.Fatalf("artifact=%s body=%s want=%s", artifact.StepID, body, wantID)
				}
			}
			if reject {
				item := record.Steps[0].Iterations[1]
				if item.Status != "skipped" || item.Child.Status != "cancelled" {
					t.Fatal("rejection left a paused child", item)
				}
			} else if len(record.Steps[0].Iterations) != 3 {
				t.Fatal("repeat scope history lost", record.Steps[0])
			}
		})
	}
}

func TestRepeatedSubworkflowExample(t *testing.T) {
	const example = "examples/workflows/repeated-subworkflow"
	for _, count := range []int{3, 5} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			dir, home := t.TempDir(), t.TempDir()
			err := os.MkdirAll(filepath.Join(dir, example), 0700)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"recipe.json", "page.json"} {
				body, readErr := os.ReadFile(filepath.Join("../..", example, name))
				if readErr != nil {
					t.Fatal(readErr)
				}
				err = os.WriteFile(filepath.Join(dir, example, name), body, 0600)
				if err != nil {
					t.Fatal(err)
				}
			}
			inputs := fmt.Sprintf(`{"count":%d}`, count)
			err = os.WriteFile(filepath.Join(dir, "inputs.json"), []byte(inputs), 0600)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = executeWorkflow(t.Context(), workflowOptions{workspace: dir, recipe: example + "/recipe.json", inputs: "inputs.json", storeDir: home}, &out, permissionGateContext(true, nil, nil))
			if err != nil {
				t.Fatal(out.String(), err)
			}
			var summary struct {
				ID      string `json:"run_id"`
				Status  string `json:"status"`
				Outputs struct {
					Last struct {
						Done bool
						Next struct{ Cursor int }
					}
				}
			}
			err = json.Unmarshal(out.Bytes(), &summary)
			if err != nil || summary.Status != "completed" || !summary.Outputs.Last.Done || summary.Outputs.Last.Next.Cursor != count {
				t.Fatal(out.String(), err)
			}
			db, err := store.OpenStore(home, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			writer, _, err := db.EditRun(summary.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			artifacts, err := writer.ListArtifacts()
			if err != nil || len(artifacts) != count {
				t.Fatalf("artifacts=%d count=%d err=%v", len(artifacts), count, err)
			}
		})
	}
}
