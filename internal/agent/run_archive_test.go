package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/llm/responses"
)

type failedArchiveClient struct{}

func (failedArchiveClient) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, &llm.APIError{Status: 401, Body: "private-key"}
}

func TestRunArchivePersistsTerminalFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	a := New(Config{Client: failedArchiveClient{}, Tools: NewRegistry(), StateFile: path, System: "sys"})
	_, err := a.Run(t.Context(), "task")
	var api *llm.APIError
	if !errors.As(err, &api) {
		t.Fatal(err)
	}
	s, err := conversation.Load(path)
	if err != nil || len(s.Runs) != 1 || s.Runs[0].Error != "Backend HTTP 401; response body omitted from saved diagnostics" {
		t.Fatalf("%+v %v", s.Runs, err)
	}
}

func TestRunArchiveSeparatesTasksAndKeepsRetryScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	client := &stubClient{responses: []llm.ChatResponse{say("first"), say("second"), say("retry")}}
	a := New(Config{Client: client, Tools: NewRegistry(), StateFile: path, System: "sys"})
	if _, err := a.Run(t.Context(), "first task"); err != nil {
		t.Fatal(err)
	}
	s, err := conversation.Load(path)
	if err != nil || len(s.Runs) != 1 || s.Runs[0].Start != 1 || s.Runs[0].End != 3 {
		t.Fatalf("first range: %+v %v", s.Runs, err)
	}
	if _, err := a.Resume(t.Context(), s.Context(), "second task"); err != nil {
		t.Fatal(err)
	}
	s, _ = conversation.Load(path)
	if len(s.Runs) != 2 || s.Runs[1].Start != 3 || s.Runs[1].End != 5 {
		t.Fatalf("second task range: %+v", s.Runs)
	}
	if _, err := a.Resume(t.Context(), s.Context(), ""); err != nil {
		t.Fatal(err)
	}
	s, _ = conversation.Load(path)
	if len(s.Runs) != 3 || s.Runs[2].Start != 3 || s.Runs[2].End != 6 || s.Runs[2].Steps != 1 {
		t.Fatalf("retry lost original task: %+v", s.Runs)
	}
}

func TestRunArchiveQuestionContinuationKeepsTask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := conversation.New([]llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "original task"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "ask", Name: "ask_user"}}},
	})
	s.Runs = []conversation.Run{{Start: 1, End: 3, Outcome: "failed", Steps: 1}}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	s, _ = conversation.Load(path)
	a := New(Config{Client: &stubClient{responses: []llm.ChatResponse{say("done")}}, Tools: NewRegistry(), StateFile: path, System: "sys"})
	if _, err := a.ResumeWithAnswer(t.Context(), s.Context(), "ask", "yes"); err != nil {
		t.Fatal(err)
	}
	s, err := conversation.Load(path)
	if err != nil || len(s.Runs) != 2 || s.Runs[1].Start != 1 || s.Runs[1].End != 5 {
		t.Fatalf("question continuation lost task: %+v %v", s.Runs, err)
	}
}

func TestResponsesPartialOutputReachesArchive(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"visible partial answer\"}\n\n")
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "state.json")
			c := responses.New(srv.URL, "test", "scope", func(context.Context) (string, error) { return "test-token", nil })
			a := New(Config{Client: c, Tools: NewRegistry(), StateFile: path, System: "sys", Stream: stream})
			if _, err := a.Run(t.Context(), "task"); err == nil {
				t.Fatal("interruption succeeded")
			}
			state, err := conversation.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, m := range state.Context() {
				if m.Content == "visible partial answer" {
					found = true
					if len(m.ToolCalls) > 0 || m.ProviderState != nil {
						t.Fatal("unsafe partial state")
					}
				}
			}
			if !found {
				t.Fatal("displayed partial output missing from saved context")
			}
		})
	}
}
