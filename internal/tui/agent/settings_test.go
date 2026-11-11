package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/tui/settings"
	workspaces "github.com/keshon/gektor/internal/workspace"
)

// scriptClient supplies deterministic local model responses.
type scriptClient func(context.Context, llm.ChatRequest) (llm.ChatResponse, error)

func (f scriptClient) Chat(ctx context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	return f(ctx, r)
}

// settingsModel supplies a configured conversation and saved history for Agent tests.
func settingsModel(t *testing.T) *Model {
	t.Helper()
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("unfinished draft")
	profile := config.Profile{Version: 1, Provider: config.Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local"}, Generation: config.Generation{MaxTokens: 100, ContextLimit: 1000}, Identity: &config.Identity{UserName: "User", SystemPrompt: "Base rules"}}
	client := scriptClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{Message: llm.Message{Content: "reply"}}, nil
	})
	sessions := t.TempDir()
	workspace := t.TempDir()
	ws, err := workspaces.New(workspace)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	m.wsRoot = ws.Root()
	m.baseEnv = roles.Env{Client: client, WS: ws, MaxTokens: 100, ContextLimit: 1000}
	m.newSession = func(task, stateFile string, images []string) (*harness.Session, error) {
		return harness.New(harness.Config{Env: m.sessionEnv(), Task: task, StateFile: stateFile, Plan: m.plan, Images: images})
	}
	id, _, err := app.CompileIdentity(app.IdentityOptions{Preset: profile.Identity.PromptPreset, Name: profile.Identity.Name, Persona: profile.Identity.UserPersona, User: profile.Identity.UserName, System: profile.Identity.SystemPrompt, Post: profile.Identity.PostHistory, MaxTokens: profile.Generation.MaxTokens, ContextLimit: profile.Generation.ContextLimit, Temperature: profile.Generation.Temperature})
	if err != nil {
		t.Fatal(err)
	}
	m.config = &app.PreparedProfile{Identity: id, Directory: sessions, Profile: profile}
	m.application = &settings.Application{
		Preferences:    config.Preferences{Version: 1, Workspace: workspace},
		PreferencePath: filepath.Join(t.TempDir(), "application.json"),
	}
	m.stateFile = filepath.Join(sessions, "original", "state.json")
	m.sessionTitle = "Original"
	if err := os.MkdirAll(filepath.Dir(m.stateFile), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := []llm.Message{
		{Role: llm.RoleSystem, Content: "Base rules"},
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "reply"},
	}
	if err := agent.SaveState(m.stateFile, saved); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDirectRenameAndDeleteDefaultCancel(t *testing.T) {
	m := settingsModel(t)
	path := m.stateFile
	m.renameActive()
	m.chrome.Dialog.Form.Editor.SetValue("New title")
	m.testKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.chrome.Dialog != nil || m.sessionTitle != "New title" {
		t.Fatal("rename failed")
	}
	m.deleteActive()
	m.testKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.chrome.Dialog == nil {
		t.Fatal("delete bypassed default cancel")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, err := os.Stat(path); err != nil {
		t.Fatal("default action deleted Identity")
	}
	m.deleteActive()
	m.testKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, err := os.Stat(path); !os.IsNotExist(err) || m.sess != nil {
		t.Fatal("explicit delete failed")
	}
}
