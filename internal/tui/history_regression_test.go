package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/events"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/roles"
	"github.com/keshon/tars/internal/run"
	"github.com/keshon/tars/internal/session"
	"github.com/keshon/tars/internal/workspace"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistoryAuditToolFailureSurvivesReopen(t *testing.T) {
	history := []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c1", Name: "run_shell", Arguments: []byte(`{}`)}}}, {Role: llm.RoleTool, ToolCallID: "c1", Content: "compiler output\nerror: exit status 1"}}
	blocks := renderHistory(history)
	if len(blocks) != 1 || !blocks[0].failed {
		t.Fatalf("failed tool restored as success: %+v", blocks)
	}
}

func TestHistoryAuditStoppingAcceptsFinalToolResult(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stStopping
	m.transcript.blocks = []block{toolCardBlock("c1", "run_shell go test")}
	m.Update(eventMsg(events.Event{Name: "tool_result", Fields: map[string]any{"call_id": "c1", "text": "cancelled", "failed": true}}))
	if m.transcript.blocks[0].open || !m.transcript.blocks[0].failed {
		t.Fatal("final result dropped; cancelled tool card remains running")
	}
}

func TestHistoryAuditFinalStepKeepsFullStreamedResponse(t *testing.T) {
	m := sizeModel(t, testModel())
	body := strings.Repeat("x", 17000)
	m.handleEvent(events.Event{Name: "delta", Fields: map[string]any{"text": body[:8500]}})
	m.handleEvent(events.Event{Name: "delta", Fields: map[string]any{"text": body[8500:]}})
	m.Update(liveFlushMsg{})
	emitter := events.New(events.EventWriter(func(ev events.Event) { m.handleEvent(ev) }))
	run.EmitStep(emitter, "", 0, llm.Message{Role: llm.RoleAssistant, Content: body}, false, 25)
	if len(m.transcript.blocks) != 1 || m.transcript.blocks[0].text != body {
		t.Fatal("complete streamed response replaced by truncated final step")
	}
}

func TestRetryUsesCheckpointAfterSuccessfulToolAndBackendFailure(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := scriptClient(func(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		calls++
		switch calls {
		case 1:
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "write", Name: "write_file", Arguments: []byte(`{"path":"result.txt","content":"written"}`)}}}}, nil
		case 2:
			return llm.ChatResponse{}, errors.New("backend offline")
		case 3:
			found := false
			for _, msg := range req.Messages {
				if msg.Role == llm.RoleTool && msg.ToolCallID == "write" {
					found = true
				}
			}
			if !found {
				return llm.ChatResponse{}, errors.New("retry discarded completed tool checkpoint")
			}
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
		default:
			return llm.ChatResponse{}, errors.New("unexpected replay")
		}
	})
	m := sizeModel(t, testModel())
	m.stateFile = filepath.Join(t.TempDir(), "state.json")
	m.newSession = func(task, path string, _ []string) (*run.Session, error) {
		return run.New(run.Config{Task: task, StateFile: path, Env: roles.Env{Client: client, WS: ws}})
	}
	done := make(chan doneMsg, 1)
	m.send = func(msg tea.Msg) {
		if d, ok := msg.(doneMsg); ok {
			done <- d
		}
	}
	await := func() doneMsg {
		t.Helper()
		select {
		case d := <-done:
			return d
		case <-time.After(5 * time.Second):
			t.Fatal("run never completed")
			return doneMsg{}
		}
	}
	if err := m.startInitial("write result", nil); err != nil {
		t.Fatal(err)
	}
	failed := await()
	if failed.err == nil {
		t.Fatal("expected second-call backend failure")
	}
	m.Update(failed)
	m.command("/retry")
	recovered := await()
	if recovered.err != nil || recovered.answer != "done" {
		t.Fatalf("retry failed: %+v", recovered)
	}
	archive, err := session.ReadHistory(m.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	for _, msg := range archive {
		for _, call := range msg.ToolCalls {
			if call.Name == "write_file" {
				writes++
			}
		}
	}
	if writes != 1 {
		t.Fatalf("write appeared %d times", writes)
	}
}

func TestApplicationHistorySearchUsesSameOpeningPath(t *testing.T) {
	m := settingsModel(t)
	history := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "title"}}
	for i := 0; i < 30; i++ {
		history = append(history, llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprint("Earlier message ", i)})
	}
	target := len(history)
	history = append(history, llm.Message{Role: llm.RoleUser, Content: "UNIQUE_HISTORY_MATCH"})
	for i := 0; i < 20; i++ {
		history = append(history, llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprint("Later message ", i)})
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := agent.SaveState(path, history); err != nil {
		t.Fatal(err)
	}
	m.sessions = &sessionsState{entries: []sessionEntry{{dir: filepath.Dir(path), title: "title", historyMatch: true, matchMessage: target}}}
	m.openSelected()
	if m.modeChange != nil || m.sess == nil || m.transcript.Follow || !strings.Contains(ansi.Strip(m.transcript.Viewport.View()), "UNIQUE_HISTORY_MATCH") {
		t.Fatal("application opening lost match or restarted services")
	}
}

func TestExplicitSuccessfulToolStatusSurvivesErrorText(t *testing.T) {
	failed := false
	blocks := renderHistory([]llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c", Name: "read_file"}}}, {Role: llm.RoleTool, ToolCallID: "c", Content: "a log example\nerror: printed text", ToolFailed: &failed}})
	if len(blocks) != 1 || blocks[0].failed {
		t.Fatal("successful result inferred as error despite saved status")
	}
}
