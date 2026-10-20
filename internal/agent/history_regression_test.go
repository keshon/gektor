package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
)

type auditFailedChat struct{}

func (auditFailedChat) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, errors.New("offline")
}

func TestHistoryAuditSubmittedInputSurvivesFirstRequestFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	a := New(Config{Client: auditFailedChat{}, Tools: NewRegistry(), StateFile: path, System: "sys"})
	if _, err := a.Run(t.Context(), "submitted task"); err == nil {
		t.Fatal("expected backend error")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("submitted input not persisted: %v", err)
	}
}

func TestHistoryAuditResumeClosesInterruptedToolCalls(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys"})
	history := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "task"}, {Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "pending", Name: "run_shell", Arguments: []byte(`{"command":"go test ./..."}`)}}}}
	if _, err := a.Resume(t.Context(), history, "continue"); err != nil {
		t.Fatal(err)
	}
	pending := false
	for _, msg := range client.lastHistory {
		if msg.Role == llm.RoleAssistant && len(msg.ToolCalls) > 0 {
			pending = true
		}
		if msg.Role == llm.RoleTool && msg.ToolCallID == "pending" {
			pending = false
		}
		if msg.Role == llm.RoleUser && pending {
			t.Fatal("wire contains user message before pending tool call has a result")
		}
	}
}

func TestCompactionAndResumeKeepOneCompleteArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	history := []llm.Message{{Role: llm.RoleSystem, Content: "old system"}, {Role: llm.RoleUser, Content: "task"}}
	for i := 0; i < 20; i++ {
		history = append(history, llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprint("answer ", i)}, llm.Message{Role: llm.RoleUser, Content: fmt.Sprint("question ", i)})
	}
	if err := SaveState(path, history); err != nil {
		t.Fatal(err)
	}
	_, after, _, err := CompactFile(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	c := &stubClient{responses: []llm.ChatResponse{{Message: llm.Message{Role: llm.RoleAssistant, Content: "new answer"}}}}
	a := New(Config{Client: c, Tools: NewRegistry(), StateFile: path, System: "new system"})
	// Settings handoff clears the leading system prompt. Context selection
	// must still reuse the saved projection, rather than expand the archive.
	seed := append([]llm.Message(nil), history...)
	seed[0].Content = ""
	if _, err := a.Resume(t.Context(), seed, "next task"); err != nil {
		t.Fatal(err)
	}
	if len(c.lastHistory) != after+1 {
		t.Fatalf("sent %d messages, expected compact context %d", len(c.lastHistory), after+1)
	}
	archive, err := session.ReadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(archive) != len(history)+2 || !reflect.DeepEqual(archive[1:len(history)], history[1:]) {
		t.Fatal("resume lost, duplicated or compacted original archive")
	}
	if archive[0].Content != "new system" || archive[len(archive)-1].Content != "new answer" {
		t.Fatal("current system/answer missing")
	}
}

func TestAnswerClosesEveryInterruptedSiblingWithoutExecutingIt(t *testing.T) {
	c := &stubClient{responses: []llm.ChatResponse{{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}}}
	var results []ToolResult
	a := New(Config{Client: c, Tools: NewRegistry(), System: "sys", OnResult: func(r ToolResult) { results = append(results, r) }})
	history := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "task"}, {Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "ask", Name: "ask_user"}, {ID: "write", Name: "write_file"}}}}
	if _, err := a.ResumeWithAnswer(t.Context(), history, "ask", "approved answer"); err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Failed || !results[1].Failed {
		t.Fatalf("recovered results: %+v", results)
	}
	if c.lastHistory[len(c.lastHistory)-1].ToolCallID != "write" {
		t.Fatal("pending sibling left unresolved")
	}
}

func TestOverflowCompactionPreservesTheArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	history := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "task"}}
	for i := 0; i < 20; i++ {
		group := assistantStep(fmt.Sprint(i))
		group[0].ToolCalls[0].Arguments = []byte(`{}`)
		history = append(history, group...)
	}
	a := New(Config{Client: &overflowOnce{}, Tools: NewRegistry(), StateFile: path, System: "sys", CompactKeepSteps: 2})
	if _, err := a.Resume(t.Context(), history, "continue"); err != nil {
		t.Fatal(err)
	}
	saved, err := session.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ContextStart == 0 || len(saved.History) <= len(history) || !reflect.DeepEqual(saved.History[:len(history)], history) {
		t.Fatal("overflow recovery lost the original archive")
	}
	// A second manual reduction and continuation must still append only new
	// messages, without copying either summary into the durable archive.
	if _, _, _, err := CompactFile(path, 1); err != nil {
		t.Fatal(err)
	}
	c := &stubClient{responses: []llm.ChatResponse{{Message: llm.Message{Role: llm.RoleAssistant, Content: "next"}}}}
	a = New(Config{Client: c, Tools: NewRegistry(), StateFile: path, System: "sys"})
	if _, err := a.Resume(t.Context(), saved.History, "next task"); err != nil {
		t.Fatal(err)
	}
	after, err := session.ReadHistory(path)
	if err != nil || len(after) != len(saved.History)+2 || !reflect.DeepEqual(after[:len(saved.History)], saved.History) {
		t.Fatalf("repeated compact/resume changed archive: %v", err)
	}
}

func TestRetryReceivesPersistedVerificationFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	c := &repeatSeqClient{responses: []llm.ChatResponse{say("finished"), say("fixed")}}
	checks := 0
	a := New(Config{Client: c, Tools: NewRegistry(), StateFile: path, System: "sys", Verify: func(context.Context) (string, error) {
		checks++
		if checks == 1 {
			return "main.go:42: undefined: value", errors.New("exit status 1")
		}
		return "PASS", nil
	}})
	if _, err := a.Run(t.Context(), "task"); err == nil || len(c.reqs) != 1 {
		t.Fatalf("verification failure triggered an automatic turn or was lost: %v", err)
	}
	history, err := session.ReadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	last := history[len(history)-1]
	if last.Role != llm.RoleUser || !strings.Contains(last.Content, "main.go:42: undefined: value") {
		t.Fatal("verification diagnostics missing from checkpoint")
	}
	if _, err := a.Resume(t.Context(), history, ""); err != nil {
		t.Fatal(err)
	}
	if len(c.reqs) != 2 || checks != 2 || !reflect.DeepEqual(c.reqs[1].Messages, history) {
		t.Fatal("retry did not use the checkpoint with verification evidence")
	}
}
