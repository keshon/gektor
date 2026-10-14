package chat

import (
	"context"
	"fmt"
	"github.com/keshon/tars/internal/llm"
	"path/filepath"
	"testing"
)

func TestCompactPreservesWholeTurnsAndBackup(t *testing.T) {
	c, err := New(clientFunc(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{Message: llm.Message{Content: "reply"}}, nil
	}), Config{System: "rules", ContextLimit: 4096, MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		c.history = append(c.history, llm.Message{Role: llm.RoleUser, Content: fmt.Sprint(i)}, llm.Message{Role: llm.RoleAssistant, Content: "reply"})
	}
	path := filepath.Join(t.TempDir(), "conversation.json")
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	before, after, err := c.CompactAndSave(path, 8)
	if err != nil || before != 24 || after != 18 || c.history[0].Content != "0" || c.history[2].Content != "4" {
		t.Fatalf("%d %d %v", before, after, err)
	}
	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "before-compact-*.json"))
	if len(backups) != 1 {
		t.Fatal("missing backup")
	}
	original, err := ReadArchive(backups[0])
	if err != nil || len(original.History) != 24 {
		t.Fatal("backup lost history")
	}
	loaded, err := Open(c.client, c.config, path)
	if err != nil || len(loaded.History()) != 18 {
		t.Fatal("compacted archive cannot resume")
	}
	if _, err := loaded.Ask(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
}
func TestCompactFailureLeavesConversationUntouched(t *testing.T) {
	c := &Conversation{}
	for i := 0; i < 6; i++ {
		c.history = append(c.history, llm.Message{Role: llm.RoleUser, Content: "question"}, llm.Message{Role: llm.RoleAssistant, Content: "answer"})
	}
	_, _, err := c.CompactAndSave(filepath.Join(t.TempDir(), "missing", "conversation.json"), 1)
	if err == nil || len(c.history) != 12 {
		t.Fatal("failed save mutated history")
	}
}
