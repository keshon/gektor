package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/retrospect"
)

func TestEvaluationProgressIsIsolatedAndSafeDuringTicks(t *testing.T) {
	m := settingsModel(t)
	m.steps, m.tokens = 7, 1234
	m.openDialog("Session evaluation", nil)
	job := &evaluationJob{cancel: func() {}, started: time.Now().Add(-3 * time.Second)}
	m.evaluation = job
	var worker sync.WaitGroup
	worker.Go(func() {
		for range 100 {
			job.update(retrospect.Progress{Stage: "Waiting for model response", Step: 2, Steps: 12, Read: 4, Total: 8})
		}
	})
	for range 100 {
		m.refreshEvaluation()
	}
	worker.Wait()
	m.refreshEvaluation()
	view := strings.Join(m.dialog.lines, "\n")
	if !strings.Contains(view, "Step 2/12") || !strings.Contains(view, "inspected 4 of 8") || !strings.Contains(view, "Waiting for model response") {
		t.Fatalf("missing live counters: %s", view)
	}
	if m.steps != 7 || m.tokens != 1234 || m.input.Value() != "unfinished draft" {
		t.Fatal("evaluation progress changed original task")
	}
	m.closeDialog()
	m.openDialog("Other dialog", []string{"keep"})
	job.update(retrospect.Progress{Stage: "stale update"})
	m.refreshEvaluation()
	if strings.Join(m.dialog.lines, "") != "keep" {
		t.Fatal("cancelled evaluation changed another dialog")
	}
}

func TestEvaluationDoesNotReplaceTaskAndClosesOnEscape(t *testing.T) {
	m := settingsModel(t)
	m.steps = 7
	m.tokens = 1234
	m.ws = m.baseEnv.WS
	original, _ := os.ReadFile(m.stateFile)
	client := scriptClient(func(ctx context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{}, ctx.Err()
	})
	m.baseEnv.Client = client
	_, cmd := m.evaluate("session")
	if cmd == nil || m.evaluation == nil || !m.busy() || m.dialog == nil {
		t.Fatal("evaluation did not open a cancellable dialog")
	}
	job := m.evaluation
	m.closeDialog()
	if m.evaluation != nil {
		t.Fatal("closing dialog left evaluation running")
	}
	msg := cmd().(evaluationDoneMsg)
	if msg.job != job || !errors.Is(msg.err, context.Canceled) {
		t.Fatalf("client did not receive cancellation: %+v", msg)
	}
	m.acceptEvaluation(msg)
	if m.dialog != nil || m.steps != 7 || m.tokens != 1234 || m.input.Value() != "unfinished draft" {
		t.Fatal("stale completion changed task or draft")
	}
	current, _ := os.ReadFile(m.stateFile)
	if string(current) != string(original) {
		t.Fatal("evaluation changed session archive")
	}
}

func TestEvaluationRejectsActiveTaskAndReportsLegacyScope(t *testing.T) {
	m := settingsModel(t)
	m.ws = m.baseEnv.WS
	m.state = stRunning
	if _, cmd := m.evaluate(""); cmd != nil || m.evaluation != nil {
		t.Fatal("review started during a task")
	}
	m.state = stDone
	_, cmd := m.evaluate("")
	if cmd == nil {
		t.Fatal("bare eval did not run")
	}
	msg := cmd().(evaluationDoneMsg)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "/eval session") {
		t.Fatalf("legacy scope should be explicit: %v", msg.err)
	}
	m.acceptEvaluation(msg)
	if m.evaluation != nil || m.dialog == nil {
		t.Fatal("failure did not finish review")
	}
	if commandTakesArgument("/eval") || !runsImmediate("/eval") {
		t.Fatal("bare eval cannot execute from command picker")
	}
}

func TestEvaluationCompletionPreservesRunCounters(t *testing.T) {
	m := settingsModel(t)
	m.steps, m.tokens = 9, 4321
	_, cancel := context.WithCancel(t.Context())
	job := &evaluationJob{cancel: cancel}
	m.evaluation = job
	m.acceptEvaluation(evaluationDoneMsg{job: job, result: retrospect.Result{Scope: "run 1", Start: 2, End: 5, Read: 4, Summary: "Historical assessment."}})
	if m.evaluation != nil || m.dialog == nil || m.steps != 9 || m.tokens != 4321 || m.input.Value() != "unfinished draft" {
		t.Fatal("review completion replaced task facts")
	}
}
