package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	toolbackground "github.com/keshon/gektor/internal/tools/background"
	"github.com/keshon/gektor/internal/workspace"
)

type workflowAuthorInput struct {
	SourceDirectory string            `json:"source_directory"`
	SourceSHA256    string            `json:"source_directory_sha256"`
	ArchiveSHA256   string            `json:"archive_sha256"`
	WorkspaceSHA256 string            `json:"workspace_sha256"`
	WorkspaceFiles  map[string]string `json:"workspace_files"`
	SourceUnchanged *bool             `json:"source_unchanged_after_attempt,omitempty"`
}

type workflowAuthorEvidence struct {
	Started          time.Time               `json:"started"`
	Finished         time.Time               `json:"finished"`
	Phase            string                  `json:"phase"`
	Error            string                  `json:"error,omitempty"`
	Passed           bool                    `json:"passed"`
	MaxSteps         int                     `json:"max_steps"`
	MaxTokens        int                     `json:"max_tokens"`
	TimeoutSeconds   int                     `json:"timeout_seconds"`
	Input            workflowAuthorInput     `json:"input"`
	Checkpoint       questionCheckpoint      `json:"checkpoint"`
	PromptSHA256     string                  `json:"original_prompt_sha256"`
	TaskSHA256       string                  `json:"relocated_task_sha256"`
	RelocationNote   string                  `json:"relocation_note"`
	ResumeAnswer     string                  `json:"operator_resume_answer"`
	Metadata         evalMetadata            `json:"metadata"`
	BeforeChecks     []CheckResult           `json:"before_checks"`
	AfterChecks      []CheckResult           `json:"after_checks"`
	Answer           string                  `json:"answer"`
	Report           agent.RunReport         `json:"report"`
	Requests         []requestTiming         `json:"requests"`
	Workflow         journeyWorkflowEvidence `json:"workflow"`
	FinalStateSHA256 string                  `json:"final_state_sha256,omitempty"`
}

// This opt-in isolates workflow authoring from the previously measured repair.
// Every trial copies the actual pending question and retained repaired project.
func TestLiveWorkflowAuthor(t *testing.T) {
	from, dir := os.Getenv("GEKTOR_LIVE_AUTHOR_FROM"), os.Getenv("GEKTOR_LIVE_AUTHOR_DIR")
	if from == "" && dir == "" {
		t.Skip("set GEKTOR_LIVE_AUTHOR_FROM and GEKTOR_LIVE_AUTHOR_DIR for a live authoring trial")
	}
	if from == "" || dir == "" {
		t.Fatal("both GEKTOR_LIVE_AUTHOR_FROM and GEKTOR_LIVE_AUTHOR_DIR are required")
	}
	from, err := filepath.Abs(from)
	if err != nil {
		t.Fatal(err)
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = os.Mkdir(dir, 0700)
	if err != nil {
		t.Fatal("authoring trial needs a fresh output directory: ", err)
	}
	report := workflowAuthorEvidence{Started: time.Now(), Phase: "prepare", MaxSteps: 24, MaxTokens: 8192, TimeoutSeconds: 900}
	defer func() {
		report.Finished = time.Now()
		if report.Input.SourceSHA256 != "" {
			_, digest, sourceErr := journeyWorkspaceDigest(report.Input.SourceDirectory)
			unchanged := sourceErr == nil && digest == report.Input.SourceSHA256
			report.Input.SourceUnchanged = &unchanged
			if !unchanged {
				report.Passed = false
				if report.Error == "" {
					report.Phase = "source_preservation"
				}
				report.Error += fmt.Sprintf("; source evidence changed: %v", sourceErr)
				t.Error("source evidence changed: ", sourceErr)
			}
		}
		state := filepath.Join(dir, "resumed.state.json")
		if _, statErr := os.Stat(state); statErr == nil {
			report.FinalStateSHA256, err = fileDigest(state)
			if err != nil {
				report.Passed = false
				report.Error += "; hash final state: " + err.Error()
				t.Error(err)
			}
		}
		data, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			t.Error(marshalErr)
			return
		}
		writeErr := os.WriteFile(filepath.Join(dir, "author.json"), data, 0600)
		if writeErr != nil {
			t.Error(writeErr)
		}
	}()
	fail := func(err error) {
		report.Error = err.Error()
		t.Fatalf("%s: %v", report.Phase, err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		fail(err)
	}
	t.Chdir(root)
	report.Input, report.Checkpoint, err = prepareWorkflowAuthor(from, dir)
	if err != nil {
		fail(err)
	}
	work := filepath.Join(dir, "workspace")
	ws, err := workspace.New(work)
	if err != nil {
		fail(err)
	}
	defer ws.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	report.Phase = "prompt"
	prompt, err := os.ReadFile("eval/prompts/journey-ledger-workflow.md")
	if err != nil {
		fail(err)
	}
	report.PromptSHA256 = fmt.Sprintf("%x", sha256.Sum256(prompt))
	report.RelocationNote = "The current workspace is the copied root " + work + ". Use this root for all operations; absolute paths in earlier messages refer to the previous workspace. Do not modify the original evidence directory " + report.Input.SourceDirectory + "."
	task := string(prompt) + "\n\n" + report.RelocationNote
	report.TaskSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(task)))
	report.ResumeAnswer = "Yes, create and validate the reusable workflow. Do not approve its publication review."
	for name, data := range map[string][]byte{"workflow-task.md": prompt, "relocated-task.md": []byte(task)} {
		err = os.WriteFile(filepath.Join(dir, name), data, 0600)
		if err != nil {
			fail(err)
		}
	}
	probe := Probe{Verify: []Check{{Type: "go_acceptance", Path: "eval/verifiers/ledger.gotest"}}}
	report.Phase = "input_acceptance"
	report.BeforeChecks = checkArtifacts(ctx, probe, ws)
	for _, check := range report.BeforeChecks {
		if !check.Passed {
			fail(fmt.Errorf("independent ledger acceptance: %s", check.Output))
		}
	}

	report.Phase = "backend"
	backend := os.Getenv("GEKTOR_LIVE_BACKEND")
	if backend == "" {
		backend = "http://127.0.0.1:9931"
	}
	client := newBackend("llama", backend, "local", "", "")
	facts, err := client.Inspect(ctx, backend, 0, true)
	if err != nil {
		fail(err)
	}
	if !strings.Contains(facts.Model, "Surogate3.7") {
		fail(fmt.Errorf("unexpected model: %s", facts.Model))
	}
	report.Phase = "metadata"
	meta := evalMetadata{Backend: backend, BackendKind: "llama", Model: facts.Model,
		ContextLimit: facts.ContextLimit, MaxTokens: report.MaxTokens, Jobs: 1, Runs: 1,
		Suite: "workflow-author", KeepWorkspaces: true, ToolProfile: "baseline"}
	identity := Probe{Task: task, Seed: filepath.Join(from, "repaired-workspace"), Verify: probe.Verify}
	err = writeMeta(dir, meta, []Probe{identity})
	if err != nil {
		fail(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		fail(err)
	}
	err = json.Unmarshal(data, &report.Metadata)
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
	defer func() { report.Requests = counter.timings }()
	procs := toolbackground.New()
	defer procs.StopAll()
	var calls []string
	env := evalEnv(counter, ws, procs, facts.ContextLimit, report.MaxTokens, func(step int, msg llm.Message) {
		t.Logf("step %d: %d tool calls", step, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			calls = append(calls, call.Name)
		}
	}, func(kind string) { t.Log("nudge: ", kind) })
	env.Model, env.MaxSteps, env.WorkflowDir = facts.Model, report.MaxSteps, workflowStoreDir(ws)
	cfg := harness.Config{Env: env, Task: task, StateFile: filepath.Join(dir, "resumed.state.json"),
		ResumeAnswer: report.ResumeAnswer, VerifyCommand: "go test ./...",
		Verify: func(ctx context.Context) (string, error) { return RunShellCommand(ctx, "go test ./...", work) },
	}
	report.Phase = "author"
	report.Answer, report.Report, err = resumeJourneyQuestion(ctx, cfg, report.Checkpoint)
	// Artifact correctness is measured even when the model fails to finish.
	// These checks run after the session has stopped, within the same deadline.
	report.AfterChecks = checkArtifacts(ctx, probe, ws)
	if err != nil {
		fail(err)
	}
	if report.Report.VerificationReceipt == nil || report.Report.VerificationReceipt.Disposition != conversation.VerificationPassed {
		fail(fmt.Errorf("authoring lacks a passed harness verification receipt"))
	}
	for _, call := range calls {
		if call == "workflow_run" {
			fail(fmt.Errorf("author executed a workflow before operator execution"))
		}
	}
	report.Phase = "project_preservation"
	for _, path := range []string{"ledger.go", "cmd/ledger/main.go", "go.mod"} {
		digest, hashErr := fileDigest(filepath.Join(work, path))
		if hashErr != nil || digest != report.Input.WorkspaceFiles[path] {
			fail(fmt.Errorf("author changed repaired file %s: %v", path, hashErr))
		}
	}
	report.Phase = "final_acceptance"
	for _, check := range report.AfterChecks {
		if !check.Passed {
			fail(fmt.Errorf("ledger acceptance after authoring: %s", check.Output))
		}
	}
	report.Phase = "workflow_acceptance"
	report.Workflow, err = checkJourneyWorkflow(ctx, ws)
	if err != nil {
		fail(err)
	}
	report.Phase, report.Passed = "completed", true
}

func prepareWorkflowAuthor(from, dir string) (workflowAuthorInput, questionCheckpoint, error) {
	var input workflowAuthorInput
	var checkpoint questionCheckpoint
	source, err := filepath.EvalSymlinks(from)
	if err != nil {
		return input, checkpoint, err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return input, checkpoint, err
	}
	destination, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return input, checkpoint, err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return input, checkpoint, err
	}
	rel, err := filepath.Rel(source, destination)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return input, checkpoint, fmt.Errorf("authoring output must be outside the source directory")
	}
	input.SourceDirectory = source
	_, input.SourceSHA256, err = journeyWorkspaceDigest(source)
	if err != nil {
		return input, checkpoint, err
	}
	sourceState := filepath.Join(source, "pending.state.json")
	snapshot, err := conversation.Load(sourceState)
	if err != nil {
		return input, checkpoint, err
	}
	callID, question, pending := agent.PausedOnQuestion(nil, snapshot.History)
	if !pending || callID == "" {
		return input, checkpoint, fmt.Errorf("source archive has no actual pending question")
	}
	archive, err := os.ReadFile(sourceState)
	if err != nil {
		return input, checkpoint, err
	}
	input.ArchiveSHA256 = fmt.Sprintf("%x", sha256.Sum256(archive))
	input.WorkspaceFiles, input.WorkspaceSHA256, err = journeyWorkspaceDigest(filepath.Join(source, "repaired-workspace"))
	if err != nil {
		return input, checkpoint, err
	}
	work := filepath.Join(destination, "workspace")
	err = copyTree(filepath.Join(source, "repaired-workspace"), work)
	if err != nil {
		return input, checkpoint, err
	}
	_, copied, err := journeyWorkspaceDigest(work)
	if err != nil || copied != input.WorkspaceSHA256 {
		return input, checkpoint, fmt.Errorf("copied workspace differs: %v", err)
	}
	checkpoint = questionCheckpoint{SourceStateFile: sourceState, Path: filepath.Join(destination, "pending.state.json"),
		CallID: callID, Question: question, SHA256: input.ArchiveSHA256}
	err = os.WriteFile(checkpoint.Path, archive, 0600)
	if err != nil {
		return input, checkpoint, err
	}
	_, unchanged, err := journeyWorkspaceDigest(source)
	if err != nil || unchanged != input.SourceSHA256 {
		return input, checkpoint, fmt.Errorf("source changed while capturing authoring input: %v", err)
	}
	return input, checkpoint, nil
}

func TestWorkflowAuthorCopiesPendingQuestionAndWorkspace(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	err := os.Mkdir(filepath.Join(source, "repaired-workspace"), 0700)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(source, "repaired-workspace", "ledger.go"), []byte("repaired"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := conversation.New([]llm.Message{{Role: llm.RoleUser, Content: "ask before authoring"}, lifecycleQuestion()})
	err = snapshot.Save(filepath.Join(source, "pending.state.json"))
	if err != nil {
		t.Fatal(err)
	}
	input, checkpoint, err := prepareWorkflowAuthor(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := fileDigest(checkpoint.Path)
	if err != nil || digest != input.ArchiveSHA256 || checkpoint.CallID != "approval" {
		t.Fatalf("pending checkpoint changed: %+v, %v", checkpoint, err)
	}
	err = os.WriteFile(filepath.Join(destination, "workspace", "ledger.go"), []byte("changed copy"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, after, err := journeyWorkspaceDigest(source)
	if err != nil || after != input.SourceSHA256 {
		t.Fatalf("source mutated: %v", err)
	}
	_, _, err = prepareWorkflowAuthor(source, filepath.Join(source, "repaired-workspace"))
	if err == nil {
		t.Fatal("accepted output within source evidence")
	}
}
