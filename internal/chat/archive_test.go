package chat

import (
	"context"
	"github.com/keshon/tars/internal/llm"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestArchiveRoundTripAndSettingsBoundary(t *testing.T) {
	client := clientFunc(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{Message: llm.Message{Content: "reply"}}, nil
	})
	cfg := Config{System: "character prompt", MaxTokens: 100, ContextLimit: 1000}
	c, err := New(client, cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "chat with spaces", "conversation.json")
	for _, text := range []string{"first", "second"} {
		if _, err = c.AskAndSave(context.Background(), text, path); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := Open(client, cfg, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.History()) != 4 {
		t.Fatal("lost turn")
	}
	cfg.System = "different"
	if _, err = Open(client, cfg, path); err == nil {
		t.Fatal("silently changed saved prompt")
	}
}
func TestSaveFailureRollsBackTurn(t *testing.T) {
	c, _ := New(clientFunc(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{Message: llm.Message{Content: "reply"}}, nil
	}), Config{MaxTokens: 100, ContextLimit: 1000})
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AskAndSave(context.Background(), "hi", filepath.Join(parent, "conversation.json")); err == nil {
		t.Fatal("expected save failure")
	}
	if len(c.History()) != 0 {
		t.Fatal("failed save committed turn")
	}
}
func TestArchiveRefusesToolsAndPartialTurns(t *testing.T) {
	for _, text := range []string{
		`{"version":1,"config":{},"history":[{"Role":"user","Content":"hi"}]}`,
		`{"version":1,"config":{},"history":[{"Role":"user","Content":"hi"},{"Role":"assistant","ToolCalls":[{"Name":"run_shell"}]}]}`,
		`{"version":2,"config":{},"history":[]}`,
	} {
		path := filepath.Join(t.TempDir(), "conversation.json")
		os.WriteFile(path, []byte(text), 0600)
		if _, err := ReadArchive(path); err == nil {
			t.Fatalf("accepted invalid archive %s", text)
		}
	}
}

func TestArchivePreservesCharacterPromptSnapshot(t *testing.T) {
	client := clientFunc(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{Message: llm.Message{Content: "reply"}}, nil
	})
	cfg := Config{System: "Ada prompt", PostHistory: "Remain Ada", Greeting: "Hello Alex", CharacterName: "Ada", Temperature: .7, MaxTokens: 100, ContextLimit: 1000}
	c, _ := New(client, cfg)
	path := filepath.Join(t.TempDir(), "conversation.json")
	if _, err := c.AskAndSave(context.Background(), "Hi", path); err != nil {
		t.Fatal(err)
	}
	a, err := ReadArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Config, cfg) {
		t.Fatal("lost compiled character settings")
	}
	cfg.PostHistory = "changed"
	if _, err := Open(client, cfg, path); err == nil {
		t.Fatal("reopen silently changed post-history directive")
	}
}
