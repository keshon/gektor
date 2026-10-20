package agent

import (
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
)

func TestRunArchiveSeparatesTasksAndKeepsRetryScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	client := &stubClient{responses: []llm.ChatResponse{say("first"), say("second"), say("retry")}}
	a := New(Config{Client: client, Tools: NewRegistry(), StateFile: path, System: "sys"})
	if _, err := a.Run(t.Context(), "first task"); err != nil {
		t.Fatal(err)
	}
	s, err := session.Load(path)
	if err != nil || len(s.Runs) != 1 || s.Runs[0].Start != 1 || s.Runs[0].End != 3 {
		t.Fatalf("first range: %+v %v", s.Runs, err)
	}
	if _, err := a.Resume(t.Context(), s.Context(), "second task"); err != nil {
		t.Fatal(err)
	}
	s, _ = session.Load(path)
	if len(s.Runs) != 2 || s.Runs[1].Start != 3 || s.Runs[1].End != 5 {
		t.Fatalf("second task range: %+v", s.Runs)
	}
	if _, err := a.Resume(t.Context(), s.Context(), ""); err != nil {
		t.Fatal(err)
	}
	s, _ = session.Load(path)
	if len(s.Runs) != 3 || s.Runs[2].Start != 3 || s.Runs[2].End != 6 || s.Runs[2].Steps != 1 {
		t.Fatalf("retry lost original task: %+v", s.Runs)
	}
}

func TestRunArchiveQuestionContinuationKeepsTask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := session.New([]llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "original task"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "ask", Name: "ask_user"}}},
	})
	s.Runs = []session.Run{{Start: 1, End: 3, Outcome: "failed", Steps: 1}}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	s, _ = session.Load(path)
	a := New(Config{Client: &stubClient{responses: []llm.ChatResponse{say("done")}}, Tools: NewRegistry(), StateFile: path, System: "sys"})
	if _, err := a.ResumeWithAnswer(t.Context(), s.Context(), "ask", "yes"); err != nil {
		t.Fatal(err)
	}
	s, err := session.Load(path)
	if err != nil || len(s.Runs) != 2 || s.Runs[1].Start != 1 || s.Runs[1].End != 5 {
		t.Fatalf("question continuation lost task: %+v %v", s.Runs, err)
	}
}
