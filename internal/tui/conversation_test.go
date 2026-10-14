package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/llm"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type conversationClient func(context.Context, llm.ChatRequest) (llm.ChatResponse, error)

func (f conversationClient) Chat(ctx context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	return f(ctx, r)
}
func TestChatTUIArchiveOpenAndToolBoundary(t *testing.T) {
	calls := 0
	client := conversationClient(func(_ context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
		calls++
		if len(r.Tools) > 0 {
			t.Error("chat offered tools")
		}
		return llm.ChatResponse{Message: llm.Message{Content: "hello"}}, nil
	})
	cfg := ChatConfig{Client: client, Settings: chat.Config{System: "persona", MaxTokens: 100, ContextLimit: 1000}, Directory: t.TempDir()}
	m := sizeModel(t, testModel())
	m.chat = &chatUI{config: cfg}
	m.state = stDone
	done := make(chan doneMsg, 1)
	m.send = func(msg tea.Msg) { done <- msg.(doneMsg) }
	if err := m.startConversation("Hi @private.txt"); err != nil {
		t.Fatal(err)
	}
	var result doneMsg
	select {
	case result = <-done:
	case <-time.After(time.Second):
		t.Fatal("chat did not complete")
	}
	m.Update(result)
	archive, err := chat.ReadArchive(m.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.History) != 2 || archive.History[0].Content != "Hi @private.txt" || calls != 1 || m.sess != nil || m.ws != nil {
		t.Fatal("chat crossed coding boundary")
	}
	entries, _ := scanSessions(cfg.Directory, nil, true)
	if len(entries) != 1 || entries[0].mode != "Chat" || len(entries[0].search) != 2 {
		t.Fatalf("session index=%+v", entries)
	}
	m.newChat()
	m.sessions = &sessionsState{entries: entries}
	m.state = stDone
	m.openSelected()
	if m.chat.current == nil || len(m.chat.current.History()) != 2 || m.sess != nil {
		t.Fatal("failed to reopen chat")
	}
	if filepath.Dir(m.stateFile) != entries[0].dir {
		t.Fatal("wrong archive")
	}
	if m.headerBadges()[0].label != "CHAT" {
		t.Fatal("chat mislabeled as coding")
	}
	if path := os.Getenv("TARS_CHAT_PREVIEW"); path != "" {
		if err := os.WriteFile(path, []byte(m.View().Content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(m.View().Content, "hello") {
		t.Fatal("completed answer absent from renderer")
	}
	m.command("/mode act")
	if m.modeName() != "Chat" || m.plan {
		t.Fatal("chat acquired coding workflow")
	}
	m.sessions = &sessionsState{entries: entries, root: cfg.Directory}
	m.note.SetValue("Renamed conversation")
	m.commitRename()
	if m.sessionTitle != "Renamed conversation" {
		t.Fatal("rename did not update active session")
	}
	m.note.SetValue("")
	m.commitRename()
	if m.sessionTitle != "Hi @private.txt" {
		t.Fatalf("clear rename lost derived title: %s", m.sessionTitle)
	}
	m.deleteSelected()
	if _, err := os.Stat(entries[0].dir); !os.IsNotExist(err) || m.chat.current != nil {
		t.Fatal("delete did not unload chat archive")
	}
	m.header.menu = "workflow"
	for _, item := range m.rawHeaderItems() {
		if (item.command == "/mode act" || item.command == "/mode plan") && item.enabled {
			t.Fatal("coding menu enabled")
		}
	}
}

func TestCharacterGreetingIsVisibleInNewChat(t *testing.T) {
	m := sizeModel(t, testModel())
	m.chat = &chatUI{config: ChatConfig{Settings: chat.Config{Greeting: "Hello Alex", CharacterName: "Ada"}}}
	m.state = stDone
	m.newChat()
	if !strings.Contains(m.View().Content, "Hello Alex") {
		t.Fatal("greeting not rendered after new chat")
	}
}
