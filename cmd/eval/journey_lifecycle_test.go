package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	toolbackground "github.com/keshon/gektor/internal/tools/background"
)

type lifecycleClient struct {
	responses []llm.Message
	requests  []llm.ChatRequest
}

func (c *lifecycleClient) Chat(_ context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	c.requests = append(c.requests, request)
	if len(c.responses) == 0 {
		return llm.ChatResponse{}, errors.New("unexpected model request")
	}
	message := c.responses[0]
	c.responses = c.responses[1:]
	return llm.ChatResponse{Message: message}, nil
}

func lifecycleConfig(t *testing.T, client *lifecycleClient) harness.Config {
	t.Helper()
	procs := toolbackground.New()
	t.Cleanup(procs.StopAll)
	return harness.Config{
		Env: roles.Env{
			Client: client, WS: checkWorkspaceForTest(t), Procs: procs,
			ContextLimit: 32768, MaxTokens: 2048, MaxSteps: 8,
			WorkflowDir: t.TempDir(),
		},
		Task: "repair the file", StateFile: filepath.Join(t.TempDir(), "run.json"),
	}
}

func lifecycleQuestion() llm.Message {
	return llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
		{ID: "approval", Name: "ask_user", Arguments: []byte(`{"question":"May I author a workflow?"}`)},
		{ID: "withheld", Name: "write_file", Arguments: []byte(`{"path":"forbidden.txt","content":"must not execute"}`)},
	}}
}

func TestJourneyLifecyclePreservesRepairAndPendingCheckpoint(t *testing.T) {
	client := &lifecycleClient{responses: []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "repair", Name: "write_file", Arguments: []byte(`{"path":"progress.txt","content":"repaired"}`)}}},
		{Role: llm.RoleAssistant, Content: "Repair complete."},
	}}
	cfg := lifecycleConfig(t, client)
	checks := 0
	cfg.Verify = func(context.Context) (string, error) {
		checks++
		content, err := os.ReadFile(filepath.Join(cfg.Env.WS.Root(), "progress.txt"))
		if err != nil {
			return "", err
		}
		if string(content) != "repaired" {
			return string(content), errors.New("repair not present")
		}
		return "repair checked", nil
	}
	session, err := harness.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if session.Report().VerificationReceipt.Disposition != conversation.VerificationPassed || checks != 1 {
		t.Fatalf("repair was not verified: %+v", session.Report())
	}
	repair, err := conversation.Load(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	client.responses = []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "checklist", Name: "todo", Arguments: []byte(`{"items":[{"id":1,"state":"pending","text":"review workflow"}]}`)}}},
		lifecycleQuestion(),
	}
	cfg.Task = "Ask before authoring a reusable workflow."
	checkpoint, err := interruptJourneyAtQuestion(t.Context(), cfg, repair.History, filepath.Join(t.TempDir(), "pending.json"))
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Report.Outcome != "cancelled" || checkpoint.Report.Final != "" || checks != 1 {
		t.Fatalf("interruption reported completion or ran verification: %+v, checks=%d", checkpoint.Report, checks)
	}
	if len(client.requests) != 4 {
		t.Fatalf("unexpected generation after cancellation: %d requests", len(client.requests))
	}
	pending, err := conversation.Load(checkpoint.Path)
	if err != nil {
		t.Fatal(err)
	}
	callID, question, paused := agent.PausedOnQuestion(nil, pending.History)
	if !paused || callID != "approval" || question != checkpoint.Question {
		t.Fatalf("checkpoint was not a genuine pending question: %s %s %v", callID, question, paused)
	}
	if len(pending.Runs) != 1 || pending.Runs[0].VerificationReceipt.Disposition != conversation.VerificationPassed {
		t.Fatalf("lost original repair receipt: %+v", pending.Runs)
	}
	interrupted, err := conversation.Load(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(interrupted.Runs) != 2 || interrupted.Runs[1].Outcome != "cancelled" {
		t.Fatalf("interrupted archive has no cancellation: %+v", interrupted.Runs)
	}
	interruptedDigest, err := fileDigest(checkpoint.SourceStateFile)
	if err != nil {
		t.Fatal(err)
	}

	resumedClient := &lifecycleClient{responses: []llm.Message{{Role: llm.RoleAssistant, Content: "Ready for separate review."}}}
	cfg.Env.Client = resumedClient
	cfg.StateFile = filepath.Join(t.TempDir(), "resumed.json")
	cfg.ResumeAnswer = "Authoring approved; publication requires separate review."
	cfg.Task = "Keep the draft private."
	cfg.Images = []string{filepath.Join(t.TempDir(), "reference.png")}
	answer, report, err := resumeJourneyQuestion(t.Context(), cfg, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Ready for separate review." || report.VerificationReceipt.Disposition != conversation.VerificationPassed || checks != 2 {
		t.Fatalf("resume did not independently verify: answer=%q report=%+v checks=%d", answer, report, checks)
	}
	if !slices.Equal(report.OpenTodos, []string{"review workflow"}) {
		t.Fatalf("accepted checklist was lost: %v", report.OpenTodos)
	}
	request := resumedClient.requests[0]
	answerIndex, siblingIndex, noteIndex := -1, -1, -1
	for i, message := range request.Messages {
		switch {
		case message.Role == llm.RoleTool && message.ToolCallID == "approval":
			answerIndex = i
			if message.Content != cfg.ResumeAnswer {
				t.Fatalf("operator answer changed: %q", message.Content)
			}
		case message.Role == llm.RoleTool && message.ToolCallID == "withheld":
			siblingIndex = i
		case message.Role == llm.RoleUser && message.Content == cfg.Task:
			noteIndex = i
			if !slices.Equal(message.Images, cfg.Images) {
				t.Fatalf("resume images lost: %v", message.Images)
			}
		}
	}
	if answerIndex < 0 || siblingIndex < 0 || noteIndex <= answerIndex || noteIndex <= siblingIndex {
		t.Fatalf("resume broke tool pairing: answer=%d sibling=%d note=%d", answerIndex, siblingIndex, noteIndex)
	}
	_, err = os.Stat(filepath.Join(cfg.Env.WS.Root(), "forbidden.txt"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unanswered sibling executed: %v", err)
	}
	digest, err := fileDigest(checkpoint.Path)
	if err != nil {
		t.Fatal(err)
	}
	if digest != checkpoint.SHA256 {
		t.Fatal("resume mutated preserved checkpoint")
	}
	digest, err = fileDigest(checkpoint.SourceStateFile)
	if err != nil {
		t.Fatal(err)
	}
	if digest != interruptedDigest {
		t.Fatal("resume mutated interrupted archive")
	}
	recovered, err := conversation.Load(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.Runs) != 2 || recovered.Runs[1].VerificationReceipt.Disposition != conversation.VerificationPassed {
		t.Fatalf("resume receipt was not persisted: %+v", recovered.Runs)
	}
}

func TestJourneyLifecycleRejectsMissingCheckpoint(t *testing.T) {
	client := &lifecycleClient{responses: []llm.Message{{Role: llm.RoleAssistant, Content: "Already done."}}}
	cfg := lifecycleConfig(t, client)
	checkpoint, err := interruptJourneyAtQuestion(t.Context(), cfg, nil, filepath.Join(t.TempDir(), "pending.json"))
	if err == nil || !strings.Contains(err.Error(), "never reached") || checkpoint.Path != "" {
		t.Fatalf("missing question accepted: checkpoint=%+v err=%v", checkpoint, err)
	}
}

func TestJourneyLifecycleRejectsCheckpointWriteFailure(t *testing.T) {
	client := &lifecycleClient{responses: []llm.Message{lifecycleQuestion()}}
	cfg := lifecycleConfig(t, client)
	checkpoint, err := interruptJourneyAtQuestion(t.Context(), cfg, nil, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "save pending") || checkpoint.Path != "" {
		t.Fatalf("checkpoint write failure accepted: checkpoint=%+v err=%v", checkpoint, err)
	}
	if checkpoint.Report.Outcome != "cancelled" || len(client.requests) != 1 {
		t.Fatalf("failed capture did not stop run: %+v", checkpoint.Report)
	}
}

func TestJourneyLifecycleResumeGuardsAndVerification(t *testing.T) {
	for _, scenario := range []string{"missing answer", "reuse checkpoint", "reuse archive", "changed checkpoint", "verification failed"} {
		t.Run(scenario, func(t *testing.T) {
			client := &lifecycleClient{responses: []llm.Message{lifecycleQuestion()}}
			cfg := lifecycleConfig(t, client)
			checkpoint, err := interruptJourneyAtQuestion(t.Context(), cfg, nil, filepath.Join(t.TempDir(), "pending.json"))
			if err != nil {
				t.Fatal(err)
			}
			resumed := &lifecycleClient{responses: []llm.Message{{Role: llm.RoleAssistant, Content: "I claim success."}}}
			cfg.Env.Client = resumed
			cfg.StateFile = filepath.Join(t.TempDir(), "resume.json")
			cfg.ResumeAnswer = "authoring only"
			switch scenario {
			case "missing answer":
				cfg.ResumeAnswer = ""
			case "reuse checkpoint":
				cfg.StateFile = checkpoint.Path
			case "reuse archive":
				cfg.StateFile = checkpoint.SourceStateFile
			case "changed checkpoint":
				err = os.WriteFile(checkpoint.Path, []byte("changed"), 0o600)
				if err != nil {
					t.Fatal(err)
				}
			case "verification failed":
				cfg.Verify = func(context.Context) (string, error) { return "oracle rejected", errors.New("incorrect result") }
			}
			answer, report, err := resumeJourneyQuestion(t.Context(), cfg, checkpoint)
			if err == nil || answer != "" {
				t.Fatalf("invalid resume accepted: answer=%q report=%+v err=%v", answer, report, err)
			}
			if scenario == "verification failed" {
				if report.Outcome != "failed" || report.Final != "" || report.VerificationReceipt.Disposition != conversation.VerificationFailed {
					t.Fatalf("model claim hid verification failure: %+v", report)
				}
			} else if len(resumed.requests) != 0 {
				t.Fatal("invalid checkpoint reached model")
			}
		})
	}
}
