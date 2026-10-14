package main

import (
	"encoding/json"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplicationDefaultsDoNotEnableSpeechOrTools(t *testing.T) {
	p, err := config.LoadPreferences(t.TempDir() + "/missing.json")
	if err != nil {
		t.Fatal(err)
	}
	p.ProfilePath = "profiles/chat.json"
	args := strings.Join(applicationArgs(p), " ")
	if strings.Contains(args, "-voice") || !strings.Contains(args, "-conversation chat") {
		t.Fatal(args)
	}
}
func TestAgentLaunchKeepsPermissionPlanBoundary(t *testing.T) {
	p := config.Preferences{Mode: "agent", Voice: "live", ProfilePath: "profile.json", Workspace: "D:/work"}
	args := strings.Join(applicationArgs(p), " ")
	for _, want := range []string{"-conversation agent", "-voice", "-plan", "-workspace D:/work"} {
		if !strings.Contains(args, want) {
			t.Fatal(args)
		}
	}
	if strings.Contains(args, "-allow") {
		t.Fatal("launch granted permissions")
	}
}
func TestAgentServiceConfigurationSupportsTextOnlyProfile(t *testing.T) {
	p := config.Profile{Version: 1, Provider: config.Provider{Kind: "llama", Ownership: "external", URL: "http://127.0.0.1:8080", Model: "local"}, Generation: config.Generation{MaxTokens: 100, ContextLimit: 1000}}
	cfg, err := configureAgentServices(p, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Configure == nil || cfg.NewVoice != nil {
		t.Fatal("text-only agent lacks settings or advertises voice")
	}
}
func TestExecutionPolicyOptionsSurviveServiceHandoff(t *testing.T) {
	args := retainedAgentOptions([]string{"-tui", "-deny", "run_shell=*", "-pure", "-mcp", "local=command", "-model", "old"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-deny run_shell=*") || !strings.Contains(joined, "-pure") || !strings.Contains(joined, "-mcp local=command") || strings.Contains(joined, "-model") {
		t.Fatal(joined)
	}
}

func TestSavedProfileIdentityAndAlternateGreetingInAgent(t *testing.T) {
	root := t.TempDir()
	cardPath := filepath.Join(root, "card.json")
	if err := os.WriteFile(cardPath, []byte(`{"name":"Ada","first_mes":"Hello","alternate_greetings":["Hi {{user}}"],"description":"{{persona}}"}`), 0600); err != nil {
		t.Fatal(err)
	}
	p := config.Profile{Version: 1, Provider: config.Provider{Kind: "llama", Ownership: "external", URL: "http://127.0.0.1:8080", Model: "local"}, Generation: config.Generation{MaxTokens: 100, ContextLimit: 2000}, Conversation: &config.Conversation{Character: cardPath, Name: "Fallback", UserName: "Alex", UserPersona: "Friend of {{char}}", GreetingIndex: 1, PromptPreset: "coding"}}
	path := filepath.Join(root, "profile.json")
	if err := config.SaveCopy(p, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := configureAgentServices(loaded, root)
	if err != nil || cfg.Settings.CharacterName != "Ada" || cfg.Settings.Greeting != "Hi Alex" || (!strings.Contains(cfg.Settings.System, "Friend of Ada") || !strings.Contains(cfg.Settings.System, "Response style (Coding)")) {
		t.Fatal(cfg.Settings, err)
	}
	loaded.Conversation.GreetingIndex = 10
	if _, err = cfg.Configure(loaded); err == nil {
		t.Fatal("invalid greeting was not rejected before handoff")
	}
	loaded.Conversation.Character = ""
	loaded.Conversation.GreetingIndex = 0
	cfg, err = cfg.Configure(loaded)
	if err != nil || cfg.Settings.CharacterName != "Fallback" {
		t.Fatal(cfg.Settings, err)
	}
}

func TestAgentLaunchLoadsCharacterFromProfile(t *testing.T) {
	captured := make(chan []llm.Message, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[{"id":"test"}]}`))
			return
		}
		var body struct {
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		captured <- body.Messages
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	root := t.TempDir()
	card := filepath.Join(root, "card.json")
	if err := os.WriteFile(card, []byte(`{"name":"Ada","description":"Friend of {{user}}"}`), 0600); err != nil {
		t.Fatal(err)
	}
	p := config.Profile{Version: 1, Provider: config.Provider{Kind: "openai", Ownership: "external", URL: server.URL + "/v1", Model: "test"}, Generation: config.Generation{MaxTokens: 128, ContextLimit: 16000}, Conversation: &config.Conversation{Character: card, UserName: "Alex", PromptPreset: "investigation"}}
	path := filepath.Join(root, "profile.json")
	if err := config.SaveCopy(p, path); err != nil {
		t.Fatal(err)
	}
	if err := runConfigured(launchRequest{args: []string{"-conversation", "agent", "-profile", path, "-workspace", root, "-no-verify", "-max-steps", "1", "Hello"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case messages := <-captured:
		if len(messages) == 0 || !strings.Contains(messages[0].Content, "Character name: Ada") || !strings.Contains(messages[0].Content, "Friend of Alex") || !strings.Contains(messages[0].Content, "Response style (Investigation)") || strings.Contains(messages[0].Content, "You are TARS") {
			t.Fatal(messages)
		}
	default:
		t.Fatal("agent did not request the configured backend")
	}
}
