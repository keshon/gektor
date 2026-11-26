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
	m.sessionsRoot = root
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
	cmd := m.sessionTitleRequest(t.Context())
	if cmd == nil {
		t.Fatal("title request not scheduled")
	}
	msg := cmd()
	m.testUpdate(msg)
	if m.sessionTitle != "Message queue bug" || conversation.ReadSessionTitle(filepath.Dir(m.stateFile)) != m.sessionTitle {
		t.Fatal(m.sessionTitle)
	}
	after, err := os.ReadFile(m.stateFile)
	if err != nil || string(before) != string(after) {
		t.Fatal("title changed conversation", err)
	}
	if m.sessionTitleRequest(t.Context()) != nil {
		t.Fatal("generated title more than once")
	}
}

func TestSessionTitlePreservesManualChangesAndDeletedSessions(t *testing.T) {
	for _, action := range []string{"rename", "same-name rename", "clear", "external rename", "delete", "switch", "failure"} {
		t.Run(action, func(t *testing.T) {
			m := titleModel(t)
			dir := filepath.Dir(m.stateFile)
			msg := m.sessionTitleRequest(t.Context())()
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
			if m.sessionTitleRequest(t.Context()) != nil {
				t.Fatal("retried after completion")
			}
		})
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
	cmd := m.sessionTitleRequest(t.Context())
	finished := make(chan sessionTitleMsg, 1)
	go func() { finished <- cmd() }()
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

func TestOldTitleResultCannotReplaceNewJob(t *testing.T) {
	m := titleModel(t)
	old := m.sessionTitleRequest(t.Context())()
	m.prepareSessionTitle("A different request")
	current := m.titleJob
	m.acceptSessionTitle(old)
	if m.titleJob != current || m.sessionTitle != "Please fix the queue bug" {
		t.Fatal("old result affected replacement job")
	}
	if cmd := m.sessionTitleRequest(t.Context()); cmd == nil {
		t.Fatal("replacement job was cancelled")
	}
	m.cancelSessionTitle()
}

func TestFreshSessionSchedulesTitleButForkKeepsItsName(t *testing.T) {
	for _, scenario := range []string{"fresh", "title failure", "fork"} {
		t.Run(scenario, func(t *testing.T) {
			fork := scenario == "fork"
			titles := 0
			root := sessionsRootFor(t, t.TempDir())
			ws, err := workspace.NewWithSkillRoots(t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			m := sizeModel(t, testModel())
			m.sessionsRoot = root
			m.state = stDone
			m.baseEnv = roles.Env{WS: ws, Client: scriptClient(func(_ context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
				if r.Messages[0].Content == prompts.SessionTitle {
					titles++
					if scenario == "title failure" {
						return llm.ChatResponse{}, errors.New("title request failed")
					}
					return llm.ChatResponse{Message: llm.Message{Content: "Workflow canvas dragging delay"}}, nil
				}
				if !fork && titles != 1 {
					t.Error("the reply started before title generation")
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
			wantTitle := "Original title"
			if !fork {
				title, ok := (<-completed).(sessionTitleMsg)
				if !ok {
					t.Fatal("title result did not arrive before the reply")
				}
				m.testUpdate(title)
				wantTitle = "Workflow canvas dragging delay"
				if scenario == "title failure" {
					wantTitle = "Inspect the dragging delay"
				}
			}
			done, ok := (<-completed).(doneMsg)
			if !ok || done.err != nil {
				t.Fatal("the first reply did not complete", done.err)
			}
			if m.titleJob != nil || m.sessionTitle != wantTitle || conversation.ReadSessionTitle(filepath.Dir(m.stateFile)) != wantTitle {
				t.Fatal("unexpected saved title", m.sessionTitle, wantTitle)
			}
		})
	}
}
