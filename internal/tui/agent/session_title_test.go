package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/workspace"
)

func titleModel(t *testing.T) *Model {
	t.Helper()
	root := sessionsRootFor(t, t.TempDir())
	dir := writeSessionState(t, root, "title-test", userHistory("Please fix the queue bug"))
	if err := conversation.WriteSessionTitle(dir, "Please fix the queue bug"); err != nil {
		t.Fatal(err)
	}
	m := sizeModel(t, testModel())
	m.state, m.stateFile, m.sessionTitle = stDone, filepath.Join(dir, "state.json"), "Please fix the queue bug"
	m.baseEnv.Client = scriptClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{Message: llm.Message{Content: "Message queue bug"}}, nil
	})
	m.prepareSessionTitle("Please fix the queue bug")
	return m
}

func TestSessionTitlePersistsWithoutChangingHistory(t *testing.T) {
	m := titleModel(t)
	before, err := os.ReadFile(m.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	cmd := m.generateSessionTitle()
	if cmd == nil {
		t.Fatal("title request not scheduled")
	}
	msg := cmd().(sessionTitleMsg)
	m.Update(msg)
	if m.sessionTitle != "Message queue bug" || conversation.ReadSessionTitle(filepath.Dir(m.stateFile)) != m.sessionTitle {
		t.Fatal(m.sessionTitle)
	}
	after, err := os.ReadFile(m.stateFile)
	if err != nil || string(before) != string(after) {
		t.Fatal("title changed conversation", err)
	}
	if m.generateSessionTitle() != nil {
		t.Fatal("generated title more than once")
	}
}

func TestSessionTitlePreservesManualChangesAndDeletedSessions(t *testing.T) {
	for _, action := range []string{"rename", "same-name rename", "clear", "external rename", "delete", "switch", "failure"} {
		t.Run(action, func(t *testing.T) {
			m := titleModel(t)
			dir := filepath.Dir(m.stateFile)
			msg := m.generateSessionTitle()().(sessionTitleMsg)
			want := "Please fix the queue bug"
			switch action {
			case "rename":
				want = "My title"
				if err := conversation.WriteSessionTitle(dir, want); err != nil {
					t.Fatal(err)
				}
				m.updateSessionTitle(dir, want)
			case "same-name rename":
				m.updateSessionTitle(dir, want)
			case "clear":
				want = ""
				if err := conversation.WriteSessionTitle(dir, ""); err != nil {
					t.Fatal(err)
				}
				m.updateSessionTitle(dir, "Please fix the queue bug")
			case "external rename":
				want = "External title"
				if err := conversation.WriteSessionTitle(dir, want); err != nil {
					t.Fatal(err)
				}
			case "delete":
				want = ""
				if err := os.RemoveAll(dir); err != nil {
					t.Fatal(err)
				}
			case "switch":
				m.stateFile = filepath.Join(t.TempDir(), "state.json")
			case "failure":
				msg.title = ""
			}
			m.acceptSessionTitle(msg)
			if got := conversation.ReadSessionTitle(dir); got != want {
				t.Fatal(action, got, want)
			}
			if action == "delete" {
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatal("recreated deleted session", err)
				}
			}
			if m.generateSessionTitle() != nil {
				t.Fatal("retried after completion")
			}
		})
	}
}

func TestStartingTurnCancelsTitleBeforeBackendCall(t *testing.T) {
	m := titleModel(t)
	m.baseEnv.Client = scriptClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		t.Error("cancelled title reached backend")
		return llm.ChatResponse{}, nil
	})
	cmd := m.generateSessionTitle()
	finished := make(chan struct{})
	m.send = func(tea.Msg) { close(finished) }
	m.startRun(func(context.Context) (string, error) { return "done", nil })
	defer m.cancel()
	<-finished
	if m.titleJob != nil {
		t.Fatal("turn did not cancel title job")
	}
	m.acceptSessionTitle(cmd().(sessionTitleMsg))
	if m.sessionTitle != "Please fix the queue bug" {
		t.Fatal("late title changed the running session")
	}
}

func TestTitleGenerationStartsOnlyAfterAnswer(t *testing.T) {
	m := titleModel(t)
	calls := 0
	m.baseEnv.Client = scriptClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		calls++
		return llm.ChatResponse{Message: llm.Message{Content: "Message queue bug"}}, nil
	})
	if calls != 0 {
		t.Fatal("title request started before answer")
	}
	_, cmd := m.Update(doneMsg{answer: "Here is the fix"})
	result := cmd()
	for {
		batch, ok := result.(tea.BatchMsg)
		if !ok {
			break
		}
		result = batch[0]()
	}
	msg, ok := result.(sessionTitleMsg)
	if !ok {
		t.Fatal("answer did not schedule title generation")
	}
	m.Update(msg)
	if calls != 1 || m.sessionTitle != "Message queue bug" {
		t.Fatal(calls, m.sessionTitle)
	}
}

func TestManualRenameCancelsInFlightTitle(t *testing.T) {
	m := titleModel(t)
	started := make(chan struct{})
	m.baseEnv.Client = scriptClient(func(ctx context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
		close(started)
		<-ctx.Done()
		return llm.ChatResponse{}, ctx.Err()
	})
	cmd := m.generateSessionTitle()
	finished := make(chan sessionTitleMsg, 1)
	go func() { finished <- cmd().(sessionTitleMsg) }()
	<-started
	dir := filepath.Dir(m.stateFile)
	if err := conversation.WriteSessionTitle(dir, "My title"); err != nil {
		t.Fatal(err)
	}
	m.updateSessionTitle(dir, "My title")
	m.acceptSessionTitle(<-finished)
	if conversation.ReadSessionTitle(dir) != "My title" || m.sessionTitle != "My title" {
		t.Fatal("late result replaced manual title")
	}
}

func TestTitleGenerationSkipsFailedInterruptedAndActiveTurns(t *testing.T) {
	for _, variant := range []string{"failed", "interrupted", "empty", "active", "externally renamed"} {
		t.Run(variant, func(t *testing.T) {
			m := titleModel(t)
			m.baseEnv.Client = scriptClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
				t.Fatal("unexpected title request")
				return llm.ChatResponse{}, nil
			})
			msg := doneMsg{answer: "An answer"}
			switch variant {
			case "failed":
				msg.err = errors.New("backend failed")
			case "interrupted":
				m.interrupted = true
			case "empty":
				msg.answer = ""
			case "active":
				m.state = stRunning
			case "externally renamed":
				if err := conversation.WriteSessionTitle(filepath.Dir(m.stateFile), "My title"); err != nil {
					t.Fatal(err)
				}
			}
			if variant == "active" || variant == "externally renamed" {
				if m.generateSessionTitle() != nil {
					t.Fatal("scheduled unwanted request")
				}
				return
			}
			m.Update(msg)
			if m.titleJob == nil || m.titleJob.cancel != nil {
				t.Fatal("title generation started without a successful answer")
			}
		})
	}
}

func TestOldTitleResultCannotReplaceNewJob(t *testing.T) {
	m := titleModel(t)
	old := m.generateSessionTitle()().(sessionTitleMsg)
	m.prepareSessionTitle("A different request")
	current := m.titleJob
	m.acceptSessionTitle(old)
	if m.titleJob != current || m.sessionTitle != "Please fix the queue bug" {
		t.Fatal("old result affected replacement job")
	}
	if cmd := m.generateSessionTitle(); cmd == nil {
		t.Fatal("replacement job was cancelled")
	}
	m.cancelSessionTitle()
}

func TestFreshSessionSchedulesTitleButForkKeepsItsName(t *testing.T) {
	for _, fork := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "fork"}[fork], func(t *testing.T) {
			sessionsRootFor(t, t.TempDir())
			ws, err := workspace.NewWithSkillRoots(t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			m := sizeModel(t, testModel())
			m.state = stDone
			m.baseEnv = roles.Env{WS: ws, Client: scriptClient(func(_ context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
				if r.Messages[0].Content == prompts.SessionTitle {
					return llm.ChatResponse{Message: llm.Message{Content: "Workflow canvas dragging delay"}}, nil
				}
				return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "Here is the answer"}}, nil
			})}
			m.newSession = func(task, stateFile string, images []string) (*harness.Session, error) {
				return harness.New(harness.Config{Env: m.baseEnv, Task: task, StateFile: stateFile, Images: images})
			}
			if fork {
				m.seedHistory = []llm.Message{{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: "Original request"}, {Role: llm.RoleAssistant, Content: "Original answer"}}
				m.sessionTitle = "Original title"
			}
			completed := make(chan tea.Msg, 1)
			m.send = func(msg tea.Msg) { completed <- msg }
			if err := m.startFreshPrompt("Inspect the dragging delay", "Inspect the dragging delay", nil); err != nil {
				t.Fatal(err)
			}
			defer m.cancel()
			done := (<-completed).(doneMsg)
			if done.err != nil {
				t.Fatal(done.err)
			}
			if fork {
				if m.titleJob != nil || conversation.ReadSessionTitle(filepath.Dir(m.stateFile)) != "Original title" {
					t.Fatal("fork scheduled automatic renaming")
				}
				return
			}
			if m.titleJob == nil {
				t.Fatal("new session missed automatic naming")
			}
			_, cmd := m.Update(done)
			result := cmd()
			for {
				batch, ok := result.(tea.BatchMsg)
				if !ok {
					break
				}
				result = batch[0]()
			}
			title, ok := result.(sessionTitleMsg)
			if !ok {
				t.Fatal("first answer missed title generation")
			}
			m.Update(title)
			if m.sessionTitle != "Workflow canvas dragging delay" {
				t.Fatal(m.sessionTitle)
			}
		})
	}
}
