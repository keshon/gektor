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
