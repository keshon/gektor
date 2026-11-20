package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/workflow"
)

// Synthetic workflow frames complement interaction tests; they do not certify
// native font fallback, mouse delivery, or platform terminal behavior.
func TestWorkflowPresentationFixtures(t *testing.T) {
	defer screen.SetTheme("dark")
	for _, theme := range []string{"dark", "light"} {
		for _, size := range [][2]int{{40, 20}, {80, 24}, {120, 40}} {
			for _, scene := range []string{"graph", "inspection", "failure", "review", "permission"} {
				name := fmt.Sprintf("workflow-%s-%s-%dx%d", scene, theme, size[0], size[1])
				t.Run(name, func(t *testing.T) {
					screen.SetTheme(theme)
					m := sizeModelTo(t, workflowTestModel(t), size[0], size[1])
					w := m.workflows
					w.editor.Replace(workflowRecipe())
					w.graph.Selected = "transform"
					want := "transform"
					switch scene {
					case "inspection":
						m.workflowCommand("inspector")
						want = "Properties"
					case "failure", "review":
						status := "failed"
						if scene == "review" {
							status = "waiting_review"
						}
						w.runID = "fixture-run"
						w.acceptPoll(workflowPoll{id: w.runID, run: &workflow.Run{RunID: w.runID, Status: status, Definition: workflowRecipe(), Steps: []workflow.StepResult{{ID: "transform", Status: status, Error: "independent acceptance rejected result", Args: json.RawMessage(`{"input":2}`)}}}})
						want = "Failed"
						if scene == "review" {
							w.run.Definition.Steps[0].Review = true
							w.reviewForm()
							want = "Review"
						}
					case "permission":
						w.acceptPermission(workflowPermission{tool: "run_program", resource: "go test ./ledger", args: json.RawMessage(`{"program":"go","args":["test","./ledger"]}`), reply: make(chan permission.Effect, 1)})
						want = "Allow operation?"
					}
					view := m.workflowScreen()
					rows := strings.Split(view, "\n")
					if len(rows) != size[1] {
						t.Fatalf("height %d != %d", len(rows), size[1])
					}
					for _, row := range rows {
						if ansi.StringWidth(row) != size[0] {
							t.Fatalf("wrong cell width: %q", row)
						}
					}
					if !strings.Contains(ansi.Strip(view), want) {
						t.Fatalf("missing active context %q:\n%s", want, ansi.Strip(view))
					}
					if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" {
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(view), 0600); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}
