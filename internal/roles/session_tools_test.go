package roles

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

type toolsetStateClient struct {
	loaded bool
	calls  int
	t      *testing.T
}

func (c *toolsetStateClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.calls++
	found := false
	for _, tool := range req.Tools {
		if tool.Name == "check_url" {
			found = true
		}
	}
	if found != c.loaded {
		c.t.Fatal("next turn has wrong tool schemas", found, c.loaded)
	}
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

func TestManualToolsetSelectionSurvivesActualConversationResume(t *testing.T) {
	e := testEnv(t)
	client := &toolsetStateClient{t: t}
	e.Client = client
	path := filepath.Join(t.TempDir(), "state.json")
	snapshot := conversation.New([]llm.Message{{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: "task"}, {Role: llm.RoleAssistant, Content: "done"}})
	for _, loaded := range []bool{false, true} {
		beforeCalls := client.calls
		var err error
		snapshot, err = e.SelectToolset(t.Context(), snapshot, "native_web", loaded)
		if err != nil {
			t.Fatal(err)
		}
		if client.calls != beforeCalls {
			t.Fatal("selection called a model")
		}
		if err = snapshot.Save(path); err != nil {
			t.Fatal(err)
		}
		client.loaded = loaded
		a, err := Interactive(e, "", path, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = a.Resume(t.Context(), snapshot.Context(), "continue"); err != nil {
			t.Fatal(err)
		}
		if client.calls != beforeCalls+1 {
			t.Fatal("selection called a model")
		}
		snapshot, err = conversation.Load(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.SelectToolset(t.Context(), snapshot, "workspace", false); err == nil {
		t.Fatal("always-loaded workspace was unloaded")
	}
}

func TestManualToolsetSelectionAppliesToFirstRun(t *testing.T) {
	for _, loaded := range []bool{false, true} {
		e := testEnv(t)
		client := &toolsetStateClient{t: t, loaded: loaded}
		e.Client = client
		snapshot, err := e.SelectToolset(t.Context(), conversation.New(nil), "native_web", loaded)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "state.json")
		err = snapshot.Save(path)
		if err != nil {
			t.Fatal(err)
		}
		a, err := Interactive(e, "", path, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = a.Run(t.Context(), "first message")
		if err != nil {
			t.Fatal(err)
		}
		if client.calls != 1 {
			t.Fatal("unexpected model calls", client.calls)
		}
	}
}
