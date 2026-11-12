package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/retrospective"
)

func TestEvaluationProgressIsIsolatedAndSafeDuringTicks(t *testing.T) {
	m := settingsModel(t)
	m.steps, m.tokens = 7, 1234
	m.testControls().Open("Session evaluation", nil)
	job := &evaluationJob{dialog: m.chrome.Dialog, cancel: func() {}, started: time.Now().Add(-3 * time.Second)}
	m.evaluation = job
	var worker sync.WaitGroup
	worker.Go(func() {
		for range 100 {
			job.update(retrospective.Progress{Stage: "Waiting for model response", Step: 2, Steps: 12, Read: 4, Total: 8})
		}
	})
	for range 100 {
		m.refreshEvaluation()
	}
	worker.Wait()
	m.refreshEvaluation()
	view := strings.Join(m.chrome.Dialog.Lines, "\n")
	if !strings.Contains(view, "Step 2/12") || !strings.Contains(view, "inspected 4 of 8") || !strings.Contains(view, "Waiting for model response") {
		t.Fatalf("missing live counters: %s", view)
	}
	if m.steps != 7 || m.tokens != 1234 || m.input.Value() != "unfinished draft" {
		t.Fatal("evaluation progress changed original task")
	}
	m.chrome.Close()
	m.syncControls()
	m.testControls().Open("Other dialog", []string{"keep"})
	job.update(retrospective.Progress{Stage: "stale update"})
	m.refreshEvaluation()
	if strings.Join(m.chrome.Dialog.Lines, "") != "keep" {
		t.Fatal("cancelled evaluation changed another dialog")
	}
}

func lessonModel(t *testing.T) (*Model, retrospective.Result) {
	t.Helper()
	m := settingsModel(t)
	m.ws = m.baseEnv.WS
	failed := false
	snapshot := conversation.New([]llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleTool, ToolFailed: &failed, Content: "PASS module tests"},
	})
	if err := snapshot.Save(m.stateFile); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(m.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	r := retrospective.Result{Start: 2, End: 2, SourceSHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Memory: []retrospective.Memory{{When: "API changes", Lesson: "Run tests inside the API module.", Evidence: []int{2}, Quote: "PASS module tests"}}}
	return m, r
}

func TestLearnReviewSaveSkipEditAndUndo(t *testing.T) {
	for _, action := range []string{"save", "skip", "edit"} {
		t.Run(action, func(t *testing.T) {
			m, r := lessonModel(t)
			m.steps, m.tokens = 7, 100
			job := &evaluationJob{learn: true, cancel: func() {}}
			m.evaluation = job
			m.acceptEvaluation(evaluationDoneMsg{job: job, result: r})
			if m.chrome.Dialog == nil || !m.chrome.Dialog.Save || !strings.Contains(strings.Join(m.chrome.Dialog.Lines, "\n"), "PASS module tests") {
				t.Fatal("proposal lacks reviewable evidence or save action")
			}
			entries, _ := m.ws.Skills()
			if len(entries) != 2 {
				t.Fatal("proposal saved before approval")
			}
			switch action {
			case "skip":
				m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
			case "save":
				m.testKey(tea.KeyPressMsg{Code: 's', Text: "s"})
			case "edit":
				m.testKey(tea.KeyPressMsg{Code: 'e', Text: "e"})
				if m.chrome.Dialog.Form == nil {
					t.Fatal("edit did not open form")
				}
				m.testControls().FocusForm(1)
				m.chrome.Dialog.Form.Editor.SetValue("Run the focused API tests first.")
				m.applyTestForm()
			}
			if m.chrome.Dialog != nil {
				t.Fatal("action did not close dialog")
			}
			if len(m.overlays) != 0 {
				t.Fatal("lesson flow leaked an overlay")
			}
			entries, err := m.ws.Skills()
			if err != nil {
				t.Fatal(err)
			}
			if action == "skip" {
				if len(entries) != 2 {
					t.Fatal("skip saved a lesson")
				}
			} else {
				if len(entries) != 3 {
					t.Fatalf("expected one lesson: %+v", entries)
				}
				lessonID := ""
				for _, entry := range entries {
					if entry.Source != "builtin" {
						lessonID = entry.ID
					}
				}
				_, guide, _, err := m.ws.LoadSkill(lessonID)
				if err != nil {
					t.Fatal(err)
				}
				if action == "edit" && !strings.Contains(guide, "focused API tests") {
					t.Fatal("saved original instead of edit")
				}
				m.learn("undo")
				entries, _ = m.ws.Skills()
				if len(entries) != 2 {
					t.Fatal("undo left active skill")
				}
			}
			if m.steps != 7 || m.tokens != 100 || m.input.Value() != "unfinished draft" {
				t.Fatal("learning changed task or draft")
			}
		})
	}
}

func TestLearnFailureKeepsReviewOpen(t *testing.T) {
	m, r := lessonModel(t)
	m.reviewLesson(r)
	if err := os.WriteFile(m.stateFile, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	m.testKey(tea.KeyPressMsg{Code: 's', Text: "s"})
	if m.chrome.Dialog == nil || !strings.Contains(strings.Join(m.chrome.Dialog.Lines, "\n"), "session changed") {
		t.Fatal("lost actionable save error")
	}
	if _, err := os.Stat(filepath.Join(m.ws.Root(), "skills")); !os.IsNotExist(err) {
		t.Fatal("failed save wrote skills")
	}
}

func TestLearnWithoutCandidateNeedsNoApproval(t *testing.T) {
	m := settingsModel(t)
	job := &evaluationJob{learn: true, cancel: func() {}}
	m.evaluation = job
	m.acceptEvaluation(evaluationDoneMsg{job: job, result: retrospective.Result{Summary: "Routine task."}})
	if m.chrome.Dialog == nil || m.chrome.Dialog.Title != "No lesson to save" || m.chrome.Dialog.Save {
		t.Fatal("empty review offered approval")
	}
}

func TestLearnReopensProposalWithoutInference(t *testing.T) {
	m, r := lessonModel(t)
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(m.stateFile), "evaluation.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	m.baseEnv.Client = scriptClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		t.Fatal("reopening a proposal invoked the model")
		return llm.ChatResponse{}, nil
	})
	_, cmd := m.learn("review")
	if cmd != nil || m.chrome.Dialog == nil || !m.chrome.Dialog.Save {
		t.Fatal("saved proposal not reopened locally")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	entries, _ := m.ws.Skills()
	if len(entries) != 2 {
		t.Fatal("Enter approved instead of skipping")
	}
}

func TestLessonPreviewFitsTerminalAndScrolls(t *testing.T) {
	for _, width := range []int{48, 80, 120} {
		m, r := lessonModel(t)
		m.testUpdate(tea.WindowSizeMsg{Width: width, Height: 24})
		r.Memory[0].Lesson = strings.Repeat("Run the focused tests before the full suite. ", 20)
		m.reviewLesson(r)
		layout := m.testControls().LayoutDialog()
		hint := ansi.Strip(layout.Hint)
		if !strings.Contains(hint, "S save") || !strings.Contains(hint, "E edit") || len(layout.Lines) <= layout.Capacity {
			t.Fatal("missing actions or scrollable long proposal")
		}
		for _, row := range strings.Split(m.testControls().DialogView(), "\n") {
			if ansi.StringWidth(row) > width {
				t.Fatalf("preview exceeds %d columns: %q", width, row)
			}
		}
		m.testControls().ScrollDialog(len(layout.Lines))
		if m.chrome.Dialog.Offset != len(layout.Lines)-layout.Capacity {
			t.Fatal("cannot reach final evidence and caveats")
		}
		m.testKey(tea.KeyPressMsg{Code: 'e', Text: "e"})
		m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		if len(m.overlays) != 0 {
			t.Fatal("edit cancellation leaked focus overlay")
		}
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
	if cmd == nil || m.evaluation == nil || !m.busy() || m.chrome.Dialog == nil {
		t.Fatal("evaluation did not open a cancellable dialog")
	}
	job := m.evaluation
	m.chrome.Close()
	m.syncControls()
	if m.evaluation != nil {
		t.Fatal("closing dialog left evaluation running")
	}
	msg := cmd().(evaluationDoneMsg)
	if msg.job != job || !errors.Is(msg.err, context.Canceled) {
		t.Fatalf("client did not receive cancellation: %+v", msg)
	}
	m.acceptEvaluation(msg)
	if m.chrome.Dialog != nil || m.steps != 7 || m.tokens != 1234 || m.input.Value() != "unfinished draft" {
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
	if m.evaluation != nil || m.chrome.Dialog == nil {
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
	m.acceptEvaluation(evaluationDoneMsg{job: job, result: retrospective.Result{Scope: "run 1", Start: 2, End: 5, Read: 4, Summary: "Historical assessment."}})
	if m.evaluation != nil || m.chrome.Dialog == nil || m.steps != 9 || m.tokens != 4321 || m.input.Value() != "unfinished draft" {
		t.Fatal("review completion replaced task facts")
	}
}

func TestEvaluationKeepsItsOwnDialogLifetime(t *testing.T) {
	for _, replace := range []bool{false, true} {
		m := settingsModel(t)
		m.ws = m.baseEnv.WS
		_, cmd := m.evaluate("")
		job := m.evaluation
		if cmd == nil || job == nil {
			t.Fatal("evaluation did not start")
		}
		m.syncControls()
		if m.evaluation != job {
			t.Fatal("opening the evaluation cancelled the new job")
		}

		if replace {
			m.chrome.Open("Replacement", []string{"keep"})
		} else {
			m.chrome.Close()
		}
		m.syncControls()
		if m.evaluation != nil {
			t.Fatal("closing or replacing its dialog retained the evaluation")
		}
	}
}
