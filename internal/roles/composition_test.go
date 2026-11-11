package roles

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
)

func TestComposedSkillReusePreservesArchive(t *testing.T) {
	e := testEnv(t)
	path := filepath.Join(e.WS.Root(), "skills", "fixture", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: fixture\ndescription: A fixture\n---\nUNIQUE_GUIDE_BODY"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	step := 0
	e.Client = extensionInspectClient(func(req llm.ChatRequest) llm.ChatResponse {
		step++
		if step <= 2 {
			return extensionCall(string(rune('a'+step)), "load_skill", `{"id":"fixture"}`)
		}
		bodies, reminders := 0, 0
		for _, m := range req.Messages {
			if m.Role == llm.RoleTool {
				if strings.Contains(m.Content, "UNIQUE_GUIDE_BODY") {
					bodies++
				}
				if strings.Contains(m.Content, "already present above") {
					reminders++
				}
			}
		}
		if bodies != 1 || reminders != 1 {
			t.Errorf("bodies=%d reminders=%d", bodies, reminders)
		}
		return extensionAnswer()
	})
	state := filepath.Join(t.TempDir(), "state.json")
	interactiveAgent, interactiveAgentErr := Interactive(e, "", state, nil, nil)
	if interactiveAgentErr != nil {
		t.Fatal(interactiveAgentErr)
	}
	if _, err := interactiveAgent.Run(t.Context(), "Load the guide twice"); err != nil {
		t.Fatal(err)
	}
	saved, err := conversation.Load(state)
	if err != nil {
		t.Fatal(err)
	}
	bodies := 0
	for _, m := range saved.History {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "UNIQUE_GUIDE_BODY") {
			bodies++
		}
	}
	if bodies != 2 {
		t.Fatal("context reuse altered stored results", bodies)
	}
}

func TestComposedQuestionRecovery(t *testing.T) {
	e := testEnv(t)
	state := filepath.Join(t.TempDir(), "state.json")
	s := conversation.New([]llm.Message{{Role: llm.RoleSystem, Content: "old"}, {Role: llm.RoleUser, Content: "original task"}, extensionCall("question", "ask_user", `{"question":"Proceed?"}`).Message})
	if err := s.Save(state); err != nil {
		t.Fatal(err)
	}
	called := false
	ask := func(context.Context, string) (string, error) { called = true; return "unexpected", nil }
	e.Client = extensionInspectClient(func(req llm.ChatRequest) llm.ChatResponse {
		found := false
		for _, m := range req.Messages {
			if m.Role == llm.RoleTool && m.ToolCallID == "question" && strings.Contains(m.Content, "yes") {
				found = true
			}
		}
		if !found {
			t.Error("recovered answer missing")
		}
		return extensionAnswer()
	})
	interactiveAgent, interactiveAgentErr := Interactive(e, "", state, ask, nil)
	if interactiveAgentErr != nil {
		t.Fatal(interactiveAgentErr)
	}
	if _, err := interactiveAgent.ResumeWithAnswer(t.Context(), s.Context(), "question", "yes"); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("resume asked the answered question again")
	}
}

type packageCheck struct{}

func (packageCheck) Check(path string, data []byte) []checks.Finding {
	if string(data) == "CHECK_ME" {
		return []checks.Finding{{Rule: "fixture-check", Path: path, Line: 1, Summary: "fixture finding"}}
	}
	return nil
}

func TestComposedWritesKeepChecksAndPathPermissions(t *testing.T) {
	e := testEnv(t)
	e.Rules = []checks.Rule{packageCheck{}}
	reports := 0
	e.Checks = checks.NewReporter(e.Rules, func(scope checks.Scope, f checks.Finding) {
		if scope == checks.PerEdit {
			reports++
		}
	})
	e.Policy = permission.Default().WithExtra([]permission.Rule{{Tool: "write_file", Pattern: "blocked.txt", Effect: permission.Deny}, {Tool: "move_file", Pattern: "destination.txt", Effect: permission.Deny}})
	write, _ := json.Marshal(map[string]string{"path": "allowed.txt", "content": "CHECK_ME"})
	blocked, _ := json.Marshal(map[string]string{"path": filepath.Join(e.WS.Root(), "blocked.txt"), "content": "blocked"})
	move, _ := json.Marshal(map[string]string{"from": "allowed.txt", "to": filepath.Join(e.WS.Root(), "destination.txt")})
	e.Client = &seqStub{steps: []llm.ChatResponse{extensionCall("write", "write_file", string(write)), extensionCall("blocked", "write_file", string(blocked)), extensionCall("move", "move_file", string(move)), extensionAnswer()}}
	interactiveAgent, interactiveAgentErr := Interactive(e, "", "", nil, nil)
	if interactiveAgentErr != nil {
		t.Fatal(interactiveAgentErr)
	}
	if _, err := interactiveAgent.Run(t.Context(), "Exercise writes"); err != nil {
		t.Fatal(err)
	}
	if reports != 1 {
		t.Fatal("post-write checks lost", reports)
	}
	if data, err := os.ReadFile(filepath.Join(e.WS.Root(), "allowed.txt")); err != nil || string(data) != "CHECK_ME" {
		t.Fatal("source moved or write failed", err)
	}
	for _, name := range []string{"blocked.txt", "destination.txt"} {
		if _, err := os.Stat(filepath.Join(e.WS.Root(), name)); !os.IsNotExist(err) {
			t.Fatal("absolute path bypassed permission", name, err)
		}
	}
}
