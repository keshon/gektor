package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/app"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
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
// A relaunch must come back up on the backend the session was created
// against. Rebuilding argv with an empty "-profile" parsed cleanly and
// then silently reverted to the flag defaults, and the profile handed
// over in memory was only read inside the profilePath != "" branch — so
// both halves dropped it.
func TestRelaunchKeepsTheProfileItRanWith(t *testing.T) {
	p := config.Preferences{Version: 1, Mode: "agent", Voice: "off", Workspace: t.TempDir()}
	args := applicationArgs(p)
	if strings.Contains(strings.Join(args, " "), "-profile") {
		t.Fatalf("a generated setup has no profile file: %q", args)
	}

	want := config.Profile{
		Version:    1,
		Provider:   config.Provider{Kind: "llama", URL: "http://10.0.0.5:8080", Model: "my-model", Ownership: "external"},
		Generation: config.Generation{MaxTokens: 4096, ContextLimit: 32768},
	}
	resolved, err := resolveLaunch(launchRequest{args: args, profile: &want})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.selectedProfile.Version != 1 {
		t.Fatalf("in-memory profile dropped: %+v", resolved.selectedProfile)
	}
	if resolved.backendURL != want.Provider.URL || resolved.model != want.Provider.Model || resolved.backendKind != want.Provider.Kind {
		t.Fatalf("relaunch lost the backend: kind=%q url=%q model=%q",
			resolved.backendKind, resolved.backendURL, resolved.model)
	}
	if resolved.maxTokens != want.Generation.MaxTokens || resolved.contextLimit != want.Generation.ContextLimit {
		t.Fatalf("relaunch lost generation: %d/%d", resolved.maxTokens, resolved.contextLimit)
	}
}

// The file still wins when nothing was handed over, and an explicit
// -profile still loads.
func TestRelaunchProfileFallbacksUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")
	want := config.Profile{
		Version:    1,
		Provider:   config.Provider{Kind: "llama", URL: "http://127.0.0.1:9999", Model: "from-file", Ownership: "external"},
		Generation: config.Generation{MaxTokens: 512, ContextLimit: 4096},
	}
	if err := config.SaveCopy(want, path); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveLaunch(launchRequest{args: []string{"-tui", "-conversation", "agent", "-profile", path, "-workspace", dir}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.model != "from-file" || resolved.backendURL != want.Provider.URL {
		t.Fatalf("explicit -profile no longer loads: kind=%q url=%q model=%q",
			resolved.backendKind, resolved.backendURL, resolved.model)
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
	cfg, err := app.ConfigureAgent(p, t.TempDir())
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


// An Agent takes no character. A card named in the profile must not reach
// the system prompt, and identity falls back to conversation.name rather
// than the card's. Identity and response style still come from the
// profile, so a shared chat profile configures an Agent without its
// persona leaking in.
func TestSavedProfileIdentityAndAlternateGreetingInAgent(t *testing.T) {
	root := t.TempDir()
	cardPath := filepath.Join(root, "card.json")
	if err := os.WriteFile(cardPath, []byte(`{"name":"Ada","first_mes":"Hello","alternate_greetings":["Hi {{user}}"],"description":"{{persona}}","system_prompt":"CARD SYSTEM","post_history_instructions":"Do not invent actions or capabilities."}`), 0600); err != nil {
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
	cfg, err := app.ConfigureAgent(loaded, root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Settings.CharacterName != "Fallback" {
		t.Fatalf("Agent identity must come from the profile, not the card: %q", cfg.Settings.CharacterName)
	}
	for _, leaked := range []string{"Ada", "CARD SYSTEM", "do not invent actions", "Hi Alex"} {
		if strings.Contains(cfg.Settings.System, leaked) || strings.Contains(cfg.Settings.PostHistory, leaked) {
			t.Fatalf("character text %q reached the Agent prompt:\n%s\n---\n%s", leaked, cfg.Settings.System, cfg.Settings.PostHistory)
		}
	}
	if cfg.Settings.Greeting != "" || len(cfg.Settings.Examples) != 0 {
		t.Fatalf("Agent must carry no greeting or dialogue examples: %q %v", cfg.Settings.Greeting, cfg.Settings.Examples)
	}
	if !strings.Contains(cfg.Settings.System, "Response style (Coding)") || !strings.Contains(cfg.Settings.System, "Friend of Fallback") {
		t.Fatalf("Agent lost its preset or persona: %q", cfg.Settings.System)
	}
	// A greeting index pointing past the end is no longer the Agent's
	// problem: the card it referred to is not being read.
	loaded.Conversation.GreetingIndex = 10
	if _, err := cfg.Configure(loaded); err != nil {
		t.Fatalf("Agent must not validate a greeting it never reads: %v", err)
	}
}

func TestAgentLaunchIgnoresCharacterFromProfile(t *testing.T) {
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
	if err := os.WriteFile(card, []byte(`{"name":"Ada","description":"Friend of {{user}}","personality":"Warm and concise."}`), 0600); err != nil {
		t.Fatal(err)
	}
	p := config.Profile{Version: 1, Provider: config.Provider{Kind: "openai", Ownership: "external", URL: server.URL + "/v1", Model: "test"}, Generation: config.Generation{MaxTokens: 128, ContextLimit: 16000}, Conversation: &config.Conversation{Character: card, Name: "Fallback", UserName: "Alex", PromptPreset: "investigation"}}
	path := filepath.Join(root, "profile.json")
	if err := config.SaveCopy(p, path); err != nil {
		t.Fatal(err)
	}
	if err := runConfigured(launchRequest{args: []string{"-conversation", "agent", "-profile", path, "-workspace", root, "-no-verify", "-max-steps", "1", "Hello"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case messages := <-captured:
		system := messages[0].Content
		for _, leaked := range []string{"Ada", "Friend of Alex", "Warm and concise"} {
			if strings.Contains(system, leaked) {
				t.Fatalf("character text %q reached the Agent system prompt:\n%s", leaked, system)
			}
		}
		if !strings.Contains(system, "Response style (Investigation)") || !strings.Contains(system, "Character name: Fallback") {
			t.Fatalf("Agent lost its profile identity:\n%s", system)
		}
		if !strings.Contains(system, "You are a local coding assistant") {
			t.Fatalf("Agent lost its operational prompt:\n%s", system)
		}
	default:
		t.Fatal("agent did not request the configured backend")
	}
}
