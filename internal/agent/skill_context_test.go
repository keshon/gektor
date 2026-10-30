package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/workspace"
)

type skillClient struct {
	calls    []llm.ToolCall
	requests [][]llm.Message
}

func (c *skillClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.requests = append(c.requests, req.Messages)
	if len(c.requests) == 1 && len(c.calls) > 0 {
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: c.calls}}, nil
	}
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

func TestSkillProjectionUsesOnlyCompleteDeliveredGuides(t *testing.T) {
	for _, variant := range []string{"complete", "truncated", "offloaded"} {
		t.Run(variant, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "skills", "release", "SKILL.md")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			body := "---\nname: release\ndescription: Release guide\n---\nCANONICAL_GUIDE_BODY"
			if variant == "truncated" {
				body += strings.Repeat("large guide body\n", 2000)
			}
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			ws, err := workspace.NewWithSkillRoots(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			client := &skillClient{}
			for _, id := range []string{"first", "second", "third"} {
				client.calls = append(client.calls, llm.ToolCall{ID: id, Name: "load_skill", Arguments: json.RawMessage(`{"id":"release"}`)})
			}
			state := filepath.Join(t.TempDir(), "state.json")
			cfg := agent.Config{Client: client, Tools: agent.NewRegistry(tools.LoadSkill{WS: ws}), System: "system", StateFile: state}
			if variant == "offloaded" {
				cfg.LimitOutput = func(context.Context, string, string, int) string {
					return "result stored externally; read_file to retrieve"
				}
			}
			if _, err := agent.New(cfg).Run(t.Context(), "load guide repeatedly"); err != nil {
				t.Fatal(err)
			}
			last := client.requests[len(client.requests)-1]
			reminders, bodies := 0, 0
			for _, msg := range last {
				if msg.Role == llm.RoleTool {
					if strings.Contains(msg.Content, "already present above") {
						reminders++
					}
					if strings.HasSuffix(msg.Content, body) {
						bodies++
					}
				}
			}
			if variant == "complete" {
				if reminders != 2 || bodies != 1 {
					t.Fatalf("delivered bodies=%d reminders=%d", bodies, reminders)
				}
				original, err := session.Load(state)
				if err != nil {
					t.Fatal(err)
				}
				for _, msg := range original.History {
					if msg.Role == llm.RoleTool && !strings.HasSuffix(msg.Content, body) {
						t.Fatal("projection changed archive")
					}
				}
				// Resume uses the same projection, then compaction drops the original
				// guide and retains a later load. The retained body must reappear.
				resumed := &skillClient{}
				cfg.Client = resumed
				cfg.StateFile = ""
				if _, err := agent.New(cfg).Resume(t.Context(), original.Context(), "continue"); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(resumed.requests[0][4].Content, "already present") {
					t.Fatal("resume lost dedup")
				}
				// Build separate assistant groups so the real compactor can drop one.
				failed := false
				group := func(id string) []llm.Message {
					return []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: "load_skill", Arguments: json.RawMessage(`{"id":"release"}`)}}}, {Role: llm.RoleTool, ToolCallID: id, Content: original.History[3].Content, ToolFailed: &failed}}
				}
				history := append([]llm.Message(nil), original.History[:2]...)
				history = append(history, group("old")...)
				history = append(history, group("kept")...)
				if err := agent.SaveState(state, history); err != nil {
					t.Fatal(err)
				}
				before, after, compacted, err := agent.CompactFile(state, 1)
				if err != nil || after >= before {
					t.Fatal(before, after, err)
				}
				resumed = &skillClient{}
				cfg.Client = resumed
				if _, err := agent.New(cfg).Resume(t.Context(), compacted, "continue"); err != nil {
					t.Fatal(err)
				}
				found := false
				for _, msg := range resumed.requests[0] {
					if msg.Role == llm.RoleTool {
						found = true
						if !strings.HasSuffix(msg.Content, body) {
							t.Fatal("compaction left false loaded state", msg.Content)
						}
					}
				}
				if !found {
					t.Fatal("missing retained tool result")
				}
			} else if reminders != 0 {
				t.Fatal("incomplete guide marked loaded")
			}
		})
	}
}
