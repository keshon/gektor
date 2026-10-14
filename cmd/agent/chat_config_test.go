package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCharacterSettingsPromptOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "card.json")
	data := `{"name":"Ada","description":"With {{user}}","first_mes":"Hello {{user}}","system_prompt":"{{original}} I am {{char}}.","post_history_instructions":"{{original}} Stay as {{char}}."}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	o := chatOptions{Character: path, User: "Alex", System: "Base.", Post: "After.", PreferMain: true, PreferPost: true, MaxTokens: 100, ContextLimit: 1000, Temperature: .7}
	cfg, _, err := compileChatSettings(o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.System, "Base. I am Ada.") || cfg.Greeting != "Hello Alex" || cfg.PostHistory != "After. Stay as Ada." || cfg.CharacterName != "Ada" || cfg.Temperature != .7 {
		t.Fatalf("bad settings: %+v", cfg)
	}
	o.PreferMain = false
	o.PreferPost = false
	cfg, _, err = compileChatSettings(o)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.System, "I am Ada") || cfg.PostHistory != "After." {
		t.Fatalf("disabled overrides applied: %+v", cfg)
	}
}

func TestProfilePersonaGreetingAndExampleCompilation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "character.json")
	data := `{"name":"Ada","description":"{{scenario}}","scenario":"With {{USER}}","first_mes":"Hello","alternate_greetings":["Hi <USER>"],"mes_example":"<START>\n{{user}}: Hi\n{{char}}: Welcome","system_prompt":"{{original}} {{persona}} {{unknown}}"}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, warnings, err := compileChatSettings(chatOptions{Character: path, User: "Alex", Persona: "A developer", GreetingIndex: 1, System: "Base", PreferMain: true, MaxTokens: 100, ContextLimit: 2000})
	if err != nil || cfg.CharacterName != "Ada" || cfg.Greeting != "Hi Alex" || len(cfg.Examples) != 2 || !strings.Contains(cfg.System, "With Alex") || !strings.Contains(cfg.System, "A developer") || len(warnings) != 1 {
		t.Fatal(cfg, warnings, err)
	}
	if strings.Contains(cfg.System, "Dialogue examples:") {
		t.Fatal("examples duplicated in system")
	}
	_, _, err = compileChatSettings(chatOptions{Character: path, User: "Alex", GreetingIndex: 2, MaxTokens: 100, ContextLimit: 2000})
	if err == nil {
		t.Fatal("invalid greeting accepted")
	}
	cfg, _, err = compileChatSettings(chatOptions{Name: "Helper", User: "Alex", System: "Speak as {{char}} to {{user}}", MaxTokens: 100, ContextLimit: 2000})
	if err != nil || cfg.CharacterName != "Helper" || !strings.Contains(cfg.System, "Speak as Helper to Alex") {
		t.Fatal(cfg, err)
	}
}

func TestPromptPresetSurvivesCardMainOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ada.json")
	if err := os.WriteFile(path, []byte(`{"name":"Ada","system_prompt":"Speak as {{char}}.","post_history_instructions":"Stay grounded."}`), 0600); err != nil {
		t.Fatal(err)
	}
	options := chatOptions{Preset: "roleplay", Character: path, User: "Alex", System: "Base", PreferMain: true, PreferPost: true, MaxTokens: 100, ContextLimit: 2000}
	settings, _, err := compileChatSettings(options)
	if err != nil || settings.CharacterName != "Ada" || !strings.Contains(settings.System, "Response style (Roleplay)") || !strings.Contains(settings.System, "Speak as Ada") || strings.Contains(settings.System, "Base") || settings.PostHistory != "Stay grounded." {
		t.Fatal(settings, err)
	}
	options.Preset = "custom"
	settings, _, err = compileChatSettings(options)
	if err != nil || strings.Contains(settings.System, "Response style") {
		t.Fatal(settings, err)
	}
	options.Preset = "unknown"
	if _, _, err = compileChatSettings(options); err == nil {
		t.Fatal("unknown preset accepted")
	}
}
