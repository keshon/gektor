package roles

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
	"github.com/keshon/gektor/internal/tools"
)

type browserProfileClient struct{}

func (browserProfileClient) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

func TestConversationRestoresBrowserReferenceWithoutStartingBrowser(t *testing.T) {
	e := testEnv(t)
	e.Client = browserProfileClient{}
	e.Browser = &tools.Browser{Channel: "chrome", Profile: "work"}
	path := filepath.Join(t.TempDir(), "state.json")
	a := Interactive(e, "", path, nil, nil)
	if _, err := a.Run(t.Context(), "task"); err != nil {
		t.Fatal(err)
	}
	saved, err := session.Load(path)
	if err != nil || saved.Browser == nil || saved.Browser.Profile != "work" || len(saved.Capabilities) != 0 {
		t.Fatal(saved, err)
	}
	// Session metadata survives a compacted projection independently of the
	// browser capability's activation history.
	projection := append([]llm.Message(nil), saved.History[:2]...)
	projection = append(projection, llm.Message{Role: llm.RoleUser, Content: "checkpoint"}, saved.History[len(saved.History)-1])
	saved.Compact(projection)
	if err := saved.Save(path); err != nil {
		t.Fatal(err)
	}
	e.Browser = &tools.Browser{Channel: "chrome"}
	a = Interactive(e, "", path, nil, nil)
	if _, err := a.Resume(t.Context(), saved.Context(), "continue"); err != nil {
		t.Fatal(err)
	}
	if e.Browser.Reference().Profile != "work" {
		t.Fatal(e.Browser.Reference())
	}
	// Reusing the launch environment for a new conversation resets selection.
	fresh := Interactive(e, "", filepath.Join(t.TempDir(), "fresh.json"), nil, nil)
	if _, err := fresh.Run(t.Context(), "new task"); err != nil {
		t.Fatal(err)
	}
	if e.Browser.Reference().Profile != "default" {
		t.Fatal(e.Browser.Reference())
	}
	if _, err := os.Stat(filepath.Join(e.WS.Root(), ".gektor", "browser")); !os.IsNotExist(err) {
		t.Fatal("browser started during metadata restore", err)
	}
}
