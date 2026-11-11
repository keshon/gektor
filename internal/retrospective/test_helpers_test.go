package retrospective

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
)

type clientFunc func(context.Context, llm.ChatRequest) (llm.ChatResponse, error)

func (f clientFunc) Chat(ctx context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	return f(ctx, r)
}

func archiveFixture(t *testing.T) (Config, conversation.Snapshot) {
	t.Helper()
	ok := false
	s := conversation.New([]llm.Message{
		{Role: llm.RoleSystem, Content: "original system"},
		{Role: llm.RoleUser, Content: "first task"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "a", Name: "run_shell", Arguments: json.RawMessage(`{"command":"go test"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "a", ToolFailed: &ok, Content: "PASS first verification"},
		{Role: llm.RoleAssistant, Content: "first answer"},
		{Role: llm.RoleUser, Content: "second task"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "b", Name: "run_shell", Arguments: json.RawMessage(`{"command":"go vet"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "b", ToolFailed: &ok, Content: "PASS second verification"},
		{Role: llm.RoleAssistant, Content: "second answer"},
	})
	s.Runs = []conversation.Run{{Start: 1, End: 5, Outcome: "completed", Steps: 2}, {Start: 5, End: 9, Outcome: "completed", Steps: 2}}
	// Two compactions hide the first run from the working context.
	s.Compact(append(append([]llm.Message(nil), s.History[:2]...), append([]llm.Message{{Role: llm.RoleUser, Content: "summary one"}}, s.History[5:]...)...))
	s.Compact(append(append([]llm.Message(nil), s.History[:2]...), llm.Message{Role: llm.RoleUser, Content: "summary two"}, s.History[8]))
	path := filepath.Join(t.TempDir(), "state.json")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("TASK_WORKSPACE_INSTRUCTIONS_MUST_NOT_ENTER_REVIEW"), 0600); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	return Config{Workspace: ws, StateFile: path}, s
}

func reviewClient(t *testing.T, start, evidence int, memory bool, beforeFinish func()) llm.Client {
	t.Helper()
	step := 0
	return clientFunc(func(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
		if len(req.Tools) != 2 {
			t.Fatalf("review received task tools: %+v", req.Tools)
		}
		names := []string{req.Tools[0].Name, req.Tools[1].Name}
		slices.Sort(names)
		if strings.Join(names, ",") != "read_history,submit_evaluation" || req.Messages[0].Content == "original system" || strings.Contains(req.Messages[0].Content, "TASK_WORKSPACE_INSTRUCTIONS_MUST_NOT_ENTER_REVIEW") {
			t.Fatalf("review inherited working tools or context: %+v", req)
		}
		step++
		var call llm.ToolCall
		switch step {
		case 1:
			args, _ := json.Marshal(map[string]int{"start": start, "count": 4})
			call = llm.ToolCall{ID: "read", Name: "read_history", Arguments: args}
		case 2:
			if last := req.Messages[len(req.Messages)-1]; last.ToolFailed == nil || *last.ToolFailed || !strings.Contains(last.Content, "verification") {
				t.Fatalf("archive read failed: %+v", last)
			}
			r := Result{Memory: []Memory{}, Summary: "Observed a successful command, not proof of general correctness.", Findings: []Finding{{Text: "Verification is archived.", Evidence: []int{evidence}}}}
			if memory {
				r.Memory = []Memory{{When: "Repeating the first check", Lesson: "The recorded command passed in this session; check current files first.", Evidence: []int{evidence}, Quote: "PASS first verification"}}
			}
			args, _ := json.Marshal(struct {
				Summary  string    `json:"summary"`
				Findings []Finding `json:"findings"`
				Memory   []Memory  `json:"memory"`
			}{r.Summary, r.Findings, r.Memory})
			call = llm.ToolCall{ID: "submit", Name: "submit_evaluation", Arguments: args}
		case 3:
			if last := req.Messages[len(req.Messages)-1]; last.ToolFailed == nil || *last.ToolFailed {
				t.Fatalf("submission failed: %+v", last)
			}
			if beforeFinish != nil {
				beforeFinish()
			}
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "Review complete."}}, nil
		default:
			t.Fatal("unexpected review continuation")
		}
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}}}, nil
	})
}
