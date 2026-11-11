package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/retrospective"
	"github.com/keshon/gektor/internal/workspace"
)

func TestFailureRecoveryHints(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{agent.ErrMaxSteps, "/max-steps"}, {agent.ErrRunBudget, "follow-up"},
		{&llm.APIError{Status: 401}, "credentials"}, {&llm.APIError{Status: 400}, "request settings"},
		{&llm.APIError{Status: 503}, "saved checkpoint"},
		{errors.New("checkpoint failed"), "try the summary again"},
	} {
		if got := recoveryHint(tc.err); !strings.Contains(got, tc.want) {
			t.Fatalf("%v: %s", tc.err, got)
		}
	}
}

func TestRunFailureAppearsOnceAndDoesNotShrinkComposer(t *testing.T) {
	m := sizeModelTo(t, testModel(), 120, 34)
	m.input.SetValue("preserved draft")
	m.fitBottom()
	height := m.transcript.Viewport.Height()
	m.testUpdate(doneMsg{err: errors.New("checkpoint failed: summary limit reached")})
	view := ansi.Strip(m.testView().Content)
	if strings.Count(view, "summary limit reached") != 1 || !strings.Contains(view, "try the summary again") {
		t.Fatal(view)
	}
	// Idle adds its normal shortcut row; the error itself stays in history.
	if m.transcript.Viewport.Height() != height-1 || m.input.Value() != "preserved draft" {
		t.Fatal("failure changed composer layout or draft")
	}
	if !strings.Contains(ansi.Strip(m.badgeLine()), "ERROR") {
		t.Fatal("persistent failure status is missing")
	}
}

func TestReceiptShowsRunnerWarningsAndRemainingWork(t *testing.T) {
	m := sizeModel(t, testModel())
	m.handleEvent(events.Event{Name: "outcome", Fields: map[string]any{
		"outcome": "incomplete", "warnings": []string{"boundary save failed"}, "open_todos": []string{"verify changes"}, "summary": "Budget expired after editing",
	}})
	b := m.transcript.blocks[len(m.transcript.blocks)-1]
	text := ansi.Strip(strings.Join(receiptText(b.text, defaultStyles(), 80), "\n"))
	for _, want := range []string{"boundary save failed", "verify changes", "Budget expired after editing"} {
		if !strings.Contains(text, want) {
			t.Fatal("receipt lost", want, text)
		}
	}
}

func TestFailedToolRespectsCompactView(t *testing.T) {
	b := block{role: roleTool, text: `run_program {"program":"go","args":["test"]}`, failed: true, result: "exit_code: 1\ncompile failed at main.go:9"}
	opts := tagged()
	opts.hideTools = true
	text := ansi.Strip(renderBlock(b, defaultStyles(), opts, 80))
	if strings.Contains(text, "compile failed at main.go:9") || !strings.Contains(text, "×") {
		t.Fatal(text)
	}
	opts.hideTools = false
	if text := ansi.Strip(renderBlock(b, defaultStyles(), opts, 80)); !strings.Contains(text, "compile failed at main.go:9") {
		t.Fatal(text)
	}
}

func TestSessionScanDistinguishesMissingBrokenAndOversize(t *testing.T) {
	root := t.TempDir()
	if _, _, err := scanSessions(filepath.Join(root, "missing"), nil, false); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(root, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := scanSessions(blocker, nil, false); err == nil {
		t.Fatal("store error hidden")
	}
	for _, id := range []string{"bad", "large"} {
		dir := filepath.Join(root, id)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("broken"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Truncate(filepath.Join(root, "large", "state.json"), 64<<20); err != nil {
		t.Fatal(err)
	}
	entries, _, err := scanSessions(root, nil, false)
	if err != nil || len(entries) != 2 {
		t.Fatalf("%v %v", entries, err)
	}
	for _, e := range entries {
		if e.preview == "" {
			t.Fatal("missing reason", e)
		}
		if e.id == "large" && e.status != "Too large to index" {
			t.Fatal(e)
		}
		if e.id == "bad" && e.status != "Unreadable" {
			t.Fatal(e)
		}
	}
}

func TestMissingImagePreservesDraft(t *testing.T) {
	m := sizeModel(t, testModel())
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	m.ws, m.state = ws, stDone
	m.input.SetValue("inspect @missing.png")
	m.followUp()
	if m.input.Value() != "inspect @missing.png" || m.state != stDone {
		t.Fatal("draft lost or model started")
	}
}

func TestLearnReviewReopensFailureWithoutInference(t *testing.T) {
	m := settingsModel(t)
	m.ws = m.baseEnv.WS
	calls := 0
	m.baseEnv.Client = scriptClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		calls++
		return llm.ChatResponse{}, nil
	})
	dir := t.TempDir()
	m.stateFile = filepath.Join(dir, "state.json")
	f := retrospective.Failure{Cause: "budget exhausted", LastValidation: "read message 4 before citing it", Progress: retrospective.Progress{Scope: "run 3", Step: 12, Steps: 12}}
	data, _ := json.Marshal(f)
	if err := os.WriteFile(filepath.Join(dir, "evaluation-failure.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.learn("review")
	if cmd != nil || calls != 0 || m.chrome.Dialog == nil || !strings.Contains(strings.Join(m.chrome.Dialog.Lines, "\n"), f.LastValidation) {
		t.Fatal("failure not reopened locally")
	}
}

func TestSavedRunFailureAppearsInSessionIndex(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "failed")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s := conversation.New([]llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "task"}})
	s.Runs = []conversation.Run{{Start: 1, End: 2, Outcome: "failed", Error: "backend disconnected"}}
	if err := s.Save(filepath.Join(dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	entries, _, err := scanSessions(root, nil, false)
	if err != nil || len(entries) != 1 || entries[0].status != "failed" || entries[0].preview != "backend disconnected" {
		t.Fatalf("%+v %v", entries, err)
	}
	m := sizeModel(t, testModel())
	m.appendSavedRun(s)
	text := m.transcript.blocks[len(m.transcript.blocks)-1].text
	if m.transcript.blocks[len(m.transcript.blocks)-1].role != roleError {
		t.Fatal("failure can be hidden with system notices")
	}
	if !strings.Contains(text, "backend disconnected") || !strings.Contains(text, "follow-up") {
		t.Fatal(text)
	}
}

func TestSessionScanErrorPreservesExistingEntries(t *testing.T) {
	m := sizeModel(t, testModel())
	old := []sessionEntry{{id: "kept", title: "saved work"}}
	m.savedSessions = old
	m.sessions = &sessionsState{entries: old, all: old, loading: true}
	m.acceptSessions(sessionsLoadedMsg{revision: m.sessionRevision, root: m.sessionRoot(), err: errors.New("store unavailable")})
	if len(m.savedSessions) != 1 || len(m.sessions.entries) != 1 || m.sessions.loading || m.sessions.flash != "store unavailable" {
		t.Fatal("scan failure hid existing sessions or its error")
	}
}
