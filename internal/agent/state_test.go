package agent

import (
	"context"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestAgent_StateFile_SavedAndLoadable(t *testing.T) {
	dir := t.TempDir()
	statePath := dir + "/state.json"
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	a := New(Config{
		Client: client,
		Tools:  NewRegistry(),
		System: "sys",

		StateFile: statePath,
	})

	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	history, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(history) == 0 {
		t.Fatal("loaded history is empty")
	}
	if history[len(history)-1].Content != "done" {
		t.Fatalf("last message content = %q, want %q", history[len(history)-1].Content, "done")
	}
}

func TestAgent_Resume_ContinuesFromLoadedHistory(t *testing.T) {
	saved := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "original task"},
		{Role: llm.RoleAssistant, Content: "partial progress"},
	}
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "finished now"}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys"})

	out, err := a.Resume(context.Background(), saved, "continue please")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if out != "finished now" {
		t.Fatalf("result = %q, want %q", out, "finished now")
	}

	found := false
	for _, m := range client.lastHistory {
		if m.Content == "continue please" {
			found = true
		}
	}
	if !found {
		t.Fatal("resume note not found in sent history")
	}
	if client.lastHistory[1].Content != "original task" {
		t.Fatal("original task message lost on resume")
	}
}

// Images ride the resume note message: the model sees text + pictures
// in one turn, and the paths persist in history for re-sends.
func TestAgent_Resume_AttachesImagesToNote(t *testing.T) {
	saved := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "original task"},
	}
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "seen"}},
	}}
	a := New(Config{Client: client, Tools: NewRegistry(), System: "sys"})

	if _, err := a.Resume(context.Background(), saved, "look", "shot.png"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	var note *llm.Message
	for i, m := range client.lastHistory {
		if m.Content == "look" {
			note = &client.lastHistory[i]
		}
	}
	if note == nil {
		t.Fatal("resume note not found in sent history")
	}
	if len(note.Images) != 1 || note.Images[0] != "shot.png" {
		t.Fatalf("note images = %v", note.Images)
	}
}
