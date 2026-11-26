package main

import (
	"bytes"
	"context"
	"encoding/json"
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

// Explicit opt-in keeps ordinary tests offline. Each invocation needs a fresh
// output directory; artifacts survive failures for inspection.
func TestLiveJourney(t *testing.T) {
	dir := os.Getenv("GEKTOR_LIVE_JOURNEY_DIR")
	if dir == "" {
		t.Skip("set GEKTOR_LIVE_JOURNEY_DIR to run against local llama")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = os.Mkdir(dir, 0700)
	if err != nil {
		t.Fatal("journey needs a new directory: ", err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	type evidence struct {
		Continuation *journeyContinuation    `json:"diagnostic_continuation,omitempty"`
		Started      time.Time               `json:"started"`
		Model        string                  `json:"model"`
		ContextLimit int                     `json:"context_limit"`
		Repair       agent.RunReport         `json:"repair"`
		RepairAnswer string                  `json:"repair_answer"`
		Checks       []CheckResult           `json:"repair_checks"`
		FinalChecks  []CheckResult           `json:"final_repair_checks"`
		Checkpoint   questionCheckpoint      `json:"checkpoint"`
		Resume       agent.RunReport         `json:"resume"`
		ResumeAnswer string                  `json:"resume_answer"`
		Workflow     journeyWorkflowEvidence `json:"workflow"`
		Requests     []requestTiming         `json:"requests"`
		Passed       bool                    `json:"passed"`
	}
	report := evidence{Started: time.Now()}
	defer func() {
		data, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			t.Error(marshalErr)
			return
		}
		writeErr := os.WriteFile(filepath.Join(dir, "journey.json"), data, 0600)
		if writeErr != nil {
			t.Error(writeErr)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	backend := os.Getenv("GEKTOR_LIVE_BACKEND")
	if backend == "" {
		backend = "http://127.0.0.1:9931"
	}
	client := newBackend("llama", backend, "local", "", "")
	facts, err := client.Inspect(ctx, backend, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(facts.Model, "Surogate3.7") {
		t.Fatal("unexpected model: ", facts.Model)
	}
	report.Model, report.ContextLimit = facts.Model, facts.ContextLimit
	trace, err := os.Create(filepath.Join(dir, "requests.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer trace.Close()
	client.SetDebug(trace)
	counter := &countingClient{inner: client}
	defer func() { report.Requests = counter.timings }()

	data, err := os.ReadFile("eval/probes/25-ledger-repair.json")
	if err != nil {
		t.Fatal(err)
	}
	var probe Probe
	err = json.Unmarshal(data, &probe)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile("eval/prompts/journey-ledger-workflow.md")
	if err != nil {
		t.Fatal(err)
	}
	meta := evalMetadata{Backend: backend, BackendKind: "llama", Model: facts.Model,
		ContextLimit: facts.ContextLimit, MaxTokens: 8192, Jobs: 1, Runs: 1, Suite: "journey",
		KeepWorkspaces: true, ToolProfile: "baseline"}
	err = writeMeta(dir, meta, []Probe{probe, {Task: string(prompt)}})
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "workflow-task.md"), prompt, 0600)
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "workspace")
	var priorHistory []llm.Message
	if from := os.Getenv("GEKTOR_LIVE_JOURNEY_FROM"); from != "" {
		report.Continuation, priorHistory, err = prepareJourneyContinuation(from, dir)
		if err != nil {
			t.Fatal(err)
		}
		err = recordJourneyContinuationMeta(dir, report.Continuation)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			sourceErr := verifyJourneyContinuationSource(report.Continuation)
			unchanged := sourceErr == nil
			report.Continuation.SourceUnchanged = &unchanged
			if sourceErr != nil {
				report.Passed = false
				t.Error(sourceErr)
			}
		}()
	} else {
		err = os.Mkdir(work, 0700)
		if err != nil {
			t.Fatal(err)
		}
		err = seedProbeWorkspace(probe, work)
		if err != nil {
			t.Fatal(err)
		}
	}
	ws, err := workspace.New(work)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	procs := toolbackground.New()
	defer procs.StopAll()
	var calls []string
	env := evalEnv(counter, ws, procs, facts.ContextLimit, 8192,
		func(step int, msg llm.Message) {
			t.Logf("step %d: %d tool calls", step, len(msg.ToolCalls))
			for _, call := range msg.ToolCalls {
				calls = append(calls, call.Name)
			}
		},
		func(kind string) { t.Log("nudge: ", kind) })
	env.Model = facts.Model
	env.MaxSteps = journeyRepairSteps
	env.WorkflowDir = workflowStoreDir(ws)
	cfg := harness.Config{
		Env:           env,
		Task:          probe.Task,
		StateFile:     filepath.Join(dir, "repair.state.json"),
		VerifyCommand: "go test ./...",
		Verify:        func(ctx context.Context) (string, error) { return RunShellCommand(ctx, "go test ./...", work) },
	}
	session, err := harness.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if report.Continuation != nil {
		report.RepairAnswer, err = session.Resume(ctx, priorHistory, report.Continuation.Note)
	} else {
		report.RepairAnswer, err = session.Run(ctx)
	}
	report.Repair = session.Report()
	if err != nil {
		t.Fatal("repair: ", err)
	}
	if report.Repair.VerificationReceipt == nil || report.Repair.VerificationReceipt.Disposition != conversation.VerificationPassed {
		t.Fatal("repair lacks a passed harness verification receipt")
	}
	report.Checks = checkArtifacts(ctx, probe, ws)
	for _, check := range report.Checks {
		if !check.Passed {
			t.Fatal("independent repair acceptance: ", check.Output)
		}
	}
	err = copyTree(work, filepath.Join(dir, "repaired-workspace"))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := conversation.Load(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg.StateFile = filepath.Join(dir, "interrupted.state.json")
	err = saved.Save(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Task = "The repair is accepted. Before creating a reusable verification/report workflow, ask me with ask_user whether to proceed. Do not author or run a workflow until I answer."
	report.Checkpoint, err = interruptJourneyAtQuestion(ctx, cfg, saved.History, filepath.Join(dir, "pending.state.json"))
	if err != nil {
		t.Fatal("checkpoint: ", err)
	}
	_, err = os.Stat(filepath.Join(work, "ledger-review.json"))
	if !os.IsNotExist(err) {
		t.Fatal("workflow appeared before operator answered")
	}
	authorStart := len(calls)
	cfg.StateFile = filepath.Join(dir, "resumed.state.json")
	cfg.ResumeAnswer = "Yes, create and validate the reusable workflow. Do not approve its publication review."
	cfg.Task = string(prompt)
	report.ResumeAnswer, report.Resume, err = resumeJourneyQuestion(ctx, cfg, report.Checkpoint)
	if err != nil {
		t.Fatal("resume: ", err)
	}
	if report.Resume.VerificationReceipt == nil || report.Resume.VerificationReceipt.Disposition != conversation.VerificationPassed {
		t.Fatal("resume lacks a passed harness verification receipt")
	}
	for _, name := range calls[authorStart:] {
		if name == "workflow_run" {
			t.Fatal("author ran a workflow before operator execution")
		}
	}
	for _, path := range []string{"ledger.go", "cmd/ledger/main.go", "go.mod"} {
		before, readErr := os.ReadFile(filepath.Join(dir, "repaired-workspace", path))
		if readErr != nil {
			t.Fatal(readErr)
		}
		after, readErr := ws.ReadFile(path)
		if readErr != nil || !bytes.Equal(before, after) {
			t.Fatalf("author changed repaired project file %s: %v", path, readErr)
		}
	}
	report.FinalChecks = checkArtifacts(ctx, probe, ws)
	for _, check := range report.FinalChecks {
		if !check.Passed {
			t.Fatal("repair acceptance after authoring: ", check.Output)
		}
	}
	report.Workflow, err = checkJourneyWorkflow(ctx, ws)
	if err != nil {
		t.Fatal("workflow acceptance: ", err)
	}
	report.Passed = true
}
