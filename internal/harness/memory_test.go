package harness

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/memory"
)

type memoryClient struct {
	requests []llm.ChatRequest
	search   bool
}

func (c *memoryClient) Chat(_ context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	c.requests = append(c.requests, request)
	message := llm.Message{Role: llm.RoleAssistant, Content: "done"}
	if c.search && len(c.requests) == 1 {
		message.Content = ""
		message.ToolCalls = []llm.ToolCall{{ID: "recall", Name: "memory_search", Arguments: json.RawMessage(`{}`)}}
	}
	return llm.ChatResponse{Message: message}, nil
}

func TestMemoryPersistsRefreshesAndStaysScopedAcrossSessions(t *testing.T) {
	directory := t.TempDir()
	client := &memoryClient{search: true}
	env := testEnv(t, client, nil)
	env.MemoryDir = directory
	other := testEnv(t, &memoryClient{}, nil)
	other.MemoryDir = directory
	store, err := memory.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	shared, _, err := store.Add(t.Context(), env.WS.Root(), memory.Input{Scope: memory.User, Text: "Prefer short replies."})
	if err != nil {
		t.Fatal(err)
	}
	project, _, err := store.Add(t.Context(), env.WS.Root(), memory.Input{Scope: memory.Project, Text: "Project Alpha uses pnpm."})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.Add(t.Context(), other.WS.Root(), memory.Input{Scope: memory.Project, Text: "Project Beta uses cargo."})
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state.json")
	session, err := New(Config{Env: env, Task: "Say done", StateFile: state})
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	first := client.requests[len(client.requests)-1]
	if !strings.Contains(first.Messages[0].Content, project.Text) || !strings.Contains(first.Messages[0].Content, shared.Text) || strings.Contains(first.Messages[0].Content, "Project Beta") {
		t.Fatal("initial memory scope is incorrect")
	}
	foundSection := false
	for _, section := range session.Report().Prompt.Sections {
		if section.Name == "memory" && section.Bytes > 0 {
			foundSection = true
		}
	}
	if !foundSection {
		t.Fatal("memory missing from context accounting")
	}
	saved, err := conversation.Load(state)
	if err != nil {
		t.Fatal(err)
	}
	// A different project receives its own facts, even when continuing an archive
	// whose system prompt and search results previously contained Alpha memory.
	secondClient := &memoryClient{}
	other.Client = secondClient
	second, err := New(Config{Env: other})
	if err != nil {
		t.Fatal(err)
	}
	_, err = second.Continue(t.Context(), saved.Context(), "Say done")
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range secondClient.requests[0].Messages {
		if strings.Contains(msg.Content, project.Text) {
			t.Fatal("project memory leaked through resumed context")
		}
	}
	if !strings.Contains(secondClient.requests[0].Messages[0].Content, "Project Beta uses cargo.") || !strings.Contains(secondClient.requests[0].Messages[0].Content, shared.Text) {
		t.Fatal("new project lost applicable memory")
	}

	updated, err := store.Update(t.Context(), env.WS.Root(), project.ID, project.Revision, memory.Input{Scope: memory.Project, Text: "Project Alpha uses bun."})
	if err != nil {
		t.Fatal(err)
	}
	followupClient := &memoryClient{}
	env.Client = followupClient
	followup, err := New(Config{Env: env, StateFile: state})
	if err != nil {
		t.Fatal(err)
	}
	_, err = followup.Resume(t.Context(), saved.Context(), "Say done")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(followupClient.requests[0].Messages[0].Content, updated.Text) {
		t.Fatal("edit not recalled on next turn")
	}
	for _, msg := range followupClient.requests[0].Messages {
		if strings.Contains(msg.Content, project.Text) {
			t.Fatal("old search result resurrected edited memory")
		}
	}
	archived, err := conversation.Load(state)
	if err != nil {
		t.Fatal(err)
	}
	keptSearch := false
	for _, msg := range archived.History {
		if msg.Role == llm.RoleTool && msg.ToolCallID == "recall" && strings.Contains(msg.Content, project.Text) {
			keptSearch = true
		}
	}
	if !keptSearch {
		t.Fatal("request projection altered archived search evidence")
	}
	err = store.Delete(t.Context(), env.WS.Root(), updated.ID, updated.Revision)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Delete(t.Context(), env.WS.Root(), shared.ID, shared.Revision)
	if err != nil {
		t.Fatal(err)
	}
	lastClient := &memoryClient{}
	env.Client = lastClient
	last, err := New(Config{Env: env})
	if err != nil {
		t.Fatal(err)
	}
	_, err = last.Continue(t.Context(), saved.Context(), "Say done")
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range lastClient.requests[0].Messages {
		if strings.Contains(msg.Content, "Project Alpha") || strings.Contains(msg.Content, shared.Text) {
			t.Fatal("deleted memory resurrected")
		}
	}
	// Projection must not erase the original archived evidence.
	if !strings.Contains(saved.History[0].Content, project.Text) {
		t.Fatal("archive memory was mutated")
	}
}

func TestMemorySearchAllowedInPlanWithoutMutationTools(t *testing.T) {
	client := &memoryClient{search: true}
	env := testEnv(t, client, nil)
	env.MemoryDir = t.TempDir()
	session, err := New(Config{Env: env, Task: "Look up saved facts", Plan: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range session.Report().RecentTools {
		if result.Name == "memory_search" && result.Status != "success" {
			t.Fatalf("read-only memory lookup blocked: %+v", result)
		}
	}
	if len(client.requests) != 2 {
		t.Fatal("lookup not executed")
	}
}
