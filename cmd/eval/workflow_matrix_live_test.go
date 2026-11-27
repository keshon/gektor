package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	toolbackground "github.com/keshon/gektor/internal/tools/background"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

type matrixTask struct{ Name, Prompt string }

func workflowMatrixTasks() []matrixTask {
	return []matrixTask{
		{"labels", "Create recipe.json, a reusable version 4 GEKTOR workflow. Discover operation contracts, save and validate it, but do not execute it. Required inputs: labels (array of strings) and prefix (string), neither with defaults. Return output labels: each input label trimmed, converted to uppercase, prefixed with the supplied prefix, preserving order and duplicates. An empty array returns an empty array. Use script_transform; do not hardcode inputs or results."},
		{"text-report", "Create recipe.json, a reusable version 4 GEKTOR workflow. Discover operation contracts, save and validate it, but do not execute or approve it. Required inputs: text (string) and report_name (string), neither with defaults. Save the supplied text in a temporary workflow artifact, read that artifact using artifact_read_text, then publish the exact text unchanged under report_name using artifact_write with review:true. Declare output report containing the published artifact metadata. Preserve Unicode, whitespace, newlines and empty strings. No shell commands or external files."},
	}
}

func TestLiveWorkflowMatrix(t *testing.T) {
	base := os.Getenv("GEKTOR_LIVE_MATRIX_DIR")
	if base == "" {
		t.Skip("set GEKTOR_LIVE_MATRIX_DIR to a fresh directory")
	}
	base, err := filepath.Abs(base)
	if err != nil {
		t.Fatal(err)
	}
	err = os.Mkdir(base, 0700)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	for _, task := range workflowMatrixTasks() {
		for attempt := 1; attempt <= 2; attempt++ {
			t.Run(fmt.Sprintf("%s-%d", task.Name, attempt), func(t *testing.T) { runWorkflowMatrix(t, base, task, attempt) })
		}
	}
}

func runWorkflowMatrix(t *testing.T, base string, task matrixTask, attempt int) {
	dir := filepath.Join(base, fmt.Sprintf("%s-%d", task.Name, attempt))
	err := os.Mkdir(dir, 0700)
	if err != nil {
		t.Fatal(err)
	}
	evidence := struct {
		MaxSteps       int             `json:"max_steps"`
		MaxTokens      int             `json:"max_tokens"`
		TimeoutSeconds int             `json:"timeout_seconds"`
		Task           string          `json:"task"`
		Started        time.Time       `json:"started"`
		Finished       time.Time       `json:"finished"`
		Error          string          `json:"error,omitempty"`
		Passed         bool            `json:"passed"`
		Report         agent.RunReport `json:"report"`
		Requests       []requestTiming `json:"requests"`
	}{Task: task.Prompt, Started: time.Now(), MaxSteps: 24, MaxTokens: 8192, TimeoutSeconds: 900}
	defer func() {
		evidence.Finished = time.Now()
		raw, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		err = os.WriteFile(filepath.Join(dir, "trial.json"), raw, 0600)
		if err != nil {
			t.Error(err)
		}
	}()
	fail := func(err error) { evidence.Error = err.Error(); t.Fatal(err) }
	err = os.Mkdir(filepath.Join(dir, "workspace"), 0700)
	if err != nil {
		fail(err)
	}
	ws, err := workspace.New(filepath.Join(dir, "workspace"))
	if err != nil {
		fail(err)
	}
	defer ws.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	backend := os.Getenv("GEKTOR_LIVE_BACKEND")
	if backend == "" {
		backend = "http://127.0.0.1:9931"
	}
	client := newBackend("llama", backend, "local", "", "")
	facts, err := client.Inspect(ctx, backend, 0, true)
	if err != nil {
		fail(err)
	}
	err = writeMeta(dir, evalMetadata{Backend: backend, BackendKind: "llama", Model: facts.Model, ContextLimit: facts.ContextLimit, MaxTokens: 8192, Jobs: 1, Runs: 1, Suite: "workflow-matrix", KeepWorkspaces: true, ToolProfile: "baseline"}, []Probe{{Task: task.Prompt}})
	if err != nil {
		fail(err)
	}
	trace, err := os.Create(filepath.Join(dir, "requests.log"))
	if err != nil {
		fail(err)
	}
	defer trace.Close()
	client.SetDebug(trace)
	counter := &countingClient{inner: client}
	defer func() { evidence.Requests = counter.timings }()
	procs := toolbackground.New()
	defer procs.StopAll()
	executed := false
	env := evalEnv(counter, ws, procs, facts.ContextLimit, 8192, func(step int, msg llm.Message) {
		t.Logf("step %d: %d tools", step, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			if call.Name == "workflow_run" {
				executed = true
			}
		}
	}, func(kind string) { t.Log("nudge:", kind) })
	env.Model, env.MaxSteps, env.WorkflowDir = facts.Model, 24, workflowStoreDir(ws)
	session, err := harness.New(harness.Config{Env: env, Task: task.Prompt, StateFile: filepath.Join(dir, "state.json")})
	if err != nil {
		fail(err)
	}
	_, err = session.Run(ctx)
	evidence.Report = session.Report()
	if err != nil {
		fail(err)
	}
	if executed {
		fail(fmt.Errorf("author executed recipe before operator"))
	}
	validated := false
	for _, call := range evidence.Report.RecentTools {
		if call.Name == "workflow_validate" && call.Status == "success" {
			validated = true
		}
		if task.Name == "text-report" && (call.Name == "run_program" || call.Name == "run_shell") {
			fail(fmt.Errorf("author used forbidden command %s", call.Name))
		}
	}
	if !validated {
		fail(fmt.Errorf("author did not successfully validate the saved recipe"))
	}
	err = checkMatrixWorkflow(ctx, ws, task.Name)
	if err != nil {
		fail(err)
	}
	evidence.Passed = true
}

func checkMatrixWorkflow(ctx context.Context, ws *workspace.Workspace, name string) error {
	source, err := ws.ReadFile("recipe.json")
	if err != nil {
		return err
	}
	directory := filepath.Join(filepath.Dir(ws.Root()), "oracle-store")
	svc, err := app.NewWorkflow(ws, directory, permission.Default(), nil)
	if err != nil {
		return err
	}
	definition, _, err := svc.Compile(ctx, source, "recipe.json")
	if err != nil {
		return err
	}
	for _, step := range definition.Steps {
		if step.Tool == "workflow_value" && (step.ID == "@inputs" || step.ID == "@outputs") {
			continue
		}
		if name == "text-report" && step.Tool != "artifact_write" && step.Tool != "artifact_read_text" && step.Tool != "script_transform" {
			return fmt.Errorf("text report uses operation outside artifact pipeline: %s", step.Tool)
		}
	}
	required := []string{"labels", "prefix"}
	if name == "text-report" {
		required = []string{"text", "report_name"}
	}
	for _, key := range required {
		spec, ok := definition.Inputs[key]
		if !ok || !spec.Required || spec.Default != nil {
			return fmt.Errorf("%s must be required without default", key)
		}
	}
	missing, err := svc.RunSource(ctx, source, "recipe.json", nil)
	if err == nil || missing.RunID != "" {
		return fmt.Errorf("missing inputs reached execution")
	}
	inputs := []map[string]json.RawMessage{
		{"labels": json.RawMessage(`[" a ","Mixed"," a "]`), "prefix": json.RawMessage(`"x:"`)},
		{"labels": json.RawMessage(`[]`), "prefix": json.RawMessage(`"other:"`)},
		{"labels": json.RawMessage(`[" b","C "]`), "prefix": json.RawMessage(`"z/"`)},
	}
	wants := []string{`{"labels":["x:A","x:MIXED","x:A"]}`, `{"labels":[]}`, `{"labels":["z/B","z/C"]}`}
	if name == "text-report" {
		inputs = []map[string]json.RawMessage{
			{"text": json.RawMessage(`" Привет\nsecond line\n"`), "report_name": json.RawMessage(`"fresh-one.txt"`)},
			{"text": json.RawMessage(`""`), "report_name": json.RawMessage(`"fresh-empty.txt"`)},
		}
	}
	for i, values := range inputs {
		result, runErr := svc.RunSource(ctx, source, "recipe.json", values)
		if runErr != nil {
			return runErr
		}
		if name == "labels" {
			if result.Status != "completed" {
				return fmt.Errorf("labels status %s", result.Status)
			}
			record, _, err := inspectJourneyRun(ctx, ws, directory, result.RunID, "")
			if err != nil {
				return err
			}
			transformed := false
			for _, step := range workflow.ScopedSteps(record) {
				if step.Definition.Tool == "script_transform" && step.Result.Status == "completed" {
					transformed = true
				}
			}
			if !transformed {
				return fmt.Errorf("labels did not execute script_transform")
			}
			var got, want any
			err = json.Unmarshal(result.Outputs, &got)
			if err != nil {
				return err
			}
			err = json.Unmarshal([]byte(wants[i]), &want)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(got, want) {
				return fmt.Errorf("labels output %s; want %s", result.Outputs, wants[i])
			}
			continue
		}
		var reportName, want string
		err = json.Unmarshal(values["report_name"], &reportName)
		if err != nil {
			return err
		}
		err = json.Unmarshal(values["text"], &want)
		if err != nil {
			return err
		}
		record, artifacts, err := inspectJourneyRun(ctx, ws, directory, result.RunID, "")
		if err != nil {
			return err
		}
		for _, artifact := range artifacts {
			if artifact.Name == reportName {
				return fmt.Errorf("published before review")
			}
		}
		review := ""
		read := false
		for _, step := range workflow.ScopedSteps(record) {
			if step.Definition.Tool == "artifact_read_text" && step.Result.Status == "completed" {
				read = true
			}
			if step.Definition.Tool == "artifact_write" && step.Definition.Review && step.Result.Status == "waiting_review" {
				review = step.ID
			}
		}
		if !read || review == "" || result.Status != "waiting_review" {
			return fmt.Errorf("missing artifact read or publication review")
		}
		approved, err := svc.Review(ctx, result.RunID, review, true)
		if err != nil {
			return err
		}
		if approved.Status != "completed" {
			return fmt.Errorf("approval status %s", approved.Status)
		}
		var outputs struct {
			Report workflow.Artifact `json:"report"`
		}
		err = json.Unmarshal(approved.Outputs, &outputs)
		if err != nil {
			return err
		}
		if outputs.Report.ID == "" || outputs.Report.Name != reportName || outputs.Report.StepID != review || outputs.Report.RunID != result.RunID {
			return fmt.Errorf("incorrect report metadata")
		}
		_, published, err := inspectJourneyRun(ctx, ws, directory, result.RunID, outputs.Report.ID)
		if err != nil {
			return err
		}
		if len(published) != 1 || published[0].Text != want {
			return fmt.Errorf("report text differs")
		}
	}
	return nil
}

func TestWorkflowMatrixOracle(t *testing.T) {
	cases := []struct{ name, recipe string }{
		{"labels", `{"version":4,"name":"Labels","inputs":{"labels":{"type":"array","required":true},"prefix":{"type":"string","required":true}},"steps":[{"id":"transform","tool":"script_transform","args":{"input":{"labels":{"$input":"labels"},"prefix":{"$input":"prefix"}},"code":"return input.labels.map(function(s){return input.prefix+s.trim().toUpperCase();});"}}],"outputs":{"labels":{"$step":"transform"}}}`},
		{"text-report", `{"version":4,"name":"Text","inputs":{"text":{"type":"string","required":true},"report_name":{"type":"string","required":true}},"steps":[{"id":"save","tool":"artifact_write","args":{"name":"temporary.txt","text":{"$input":"text"}}},{"id":"read","tool":"artifact_read_text","args":{"id":{"$step":"save","pointer":"/id"}}},{"id":"publish","tool":"artifact_write","review":true,"args":{"name":{"$input":"report_name"},"text":{"$step":"read","pointer":"/text"}}}],"outputs":{"report":{"$step":"publish"}}}`},
		{"text-report", `{"version":4,"name":"Text through a pure intermediate","inputs":{"text":{"type":"string","required":true},"report_name":{"type":"string","required":true}},"steps":[{"id":"save","tool":"artifact_write","args":{"name":"temporary.txt","text":{"$input":"text"}}},{"id":"read","tool":"artifact_read_text","args":{"id":{"$step":"save","pointer":"/id"}}},{"id":"copy","tool":"script_transform","args":{"input":{"$step":"read","pointer":"/text"},"code":"return input;"}},{"id":"publish","tool":"artifact_write","review":true,"args":{"name":{"$input":"report_name"},"text":{"$step":"copy"}}}],"outputs":{"report":{"$step":"publish"}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			err = os.WriteFile(filepath.Join(ws.Root(), "recipe.json"), []byte(tc.recipe), 0600)
			if err != nil {
				t.Fatal(err)
			}
			err = checkMatrixWorkflow(t.Context(), ws, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			var broken map[string]any
			err = json.Unmarshal([]byte(tc.recipe), &broken)
			if err != nil {
				t.Fatal(err)
			}
			broken["outputs"] = map[string]any{"wrong": "hardcoded"}
			raw, err := json.Marshal(broken)
			if err != nil {
				t.Fatal(err)
			}
			err = os.WriteFile(filepath.Join(ws.Root(), "recipe.json"), raw, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if checkMatrixWorkflow(t.Context(), ws, tc.name) == nil {
				t.Fatal("accepted wrong output")
			}
		})
	}
}

// Regrading copies retained workspaces and never changes original trial evidence.
func TestWorkflowMatrixRegrade(t *testing.T) {
	from := os.Getenv("GEKTOR_MATRIX_REGRADE_FROM")
	destination := os.Getenv("GEKTOR_MATRIX_REGRADE_DIR")
	if from == "" && destination == "" {
		t.Skip("set regrade source and fresh output directory")
	}
	if from == "" || destination == "" {
		t.Fatal("both regrade paths required")
	}
	err := os.Mkdir(destination, 0700)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range workflowMatrixTasks() {
		for attempt := 1; attempt <= 2; attempt++ {
			name := fmt.Sprintf("%s-%d", task.Name, attempt)
			t.Run(name, func(t *testing.T) {
				var original struct {
					Passed bool
					Error  string
					Report agent.RunReport
				}
				raw, err := os.ReadFile(filepath.Join(from, name, "trial.json"))
				if err != nil {
					t.Fatal(err)
				}
				err = json.Unmarshal(raw, &original)
				if err != nil {
					t.Fatal(err)
				}
				if !original.Passed {
					t.Fatalf("original did not pass: %s", original.Error)
				}
				validated := false
				for _, call := range original.Report.RecentTools {
					if call.Name == "workflow_validate" && call.Status == "success" {
						validated = true
					}
					if call.Name == "workflow_run" {
						t.Fatal("author executed workflow")
					}
					if task.Name == "text-report" && (call.Name == "run_program" || call.Name == "run_shell" || call.Name == "start_program" || call.Name == "start_background") {
						t.Fatal("author invoked command")
					}
				}
				if !validated {
					t.Fatal("author did not validate")
				}
				work := filepath.Join(destination, name, "workspace")
				err = copyTree(filepath.Join(from, name, "workspace"), work)
				if err != nil {
					t.Fatal(err)
				}
				ws, err := workspace.New(work)
				if err != nil {
					t.Fatal(err)
				}
				defer ws.Close()
				err = checkMatrixWorkflow(t.Context(), ws, task.Name)
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
