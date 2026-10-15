package main

import (
	"errors"
	"flag"
	"path/filepath"
	"testing"

	"github.com/keshon/tars/internal/config"
)

func TestParseLaunchOptionsPreservesTerminalAndAliasBehavior(t *testing.T) {
	o, err := parseLaunchOptions([]string{"-chat", "-conversation", "agent", "-grammar=false", "-character-png", "card.png", "explain", "this"})
	if err != nil {
		t.Fatal(err)
	}
	if o.chat || o.grammar || o.characterPath != "card.png" || o.task != "explain this" {
		t.Fatal("conversation selection, explicit false, alias, or task changed")
	}
	if !o.explicitFlags["character-png"] || o.explicitFlags["character"] {
		t.Fatal("alias must retain its explicit name for profile precedence")
	}
	if _, err := parseLaunchOptions([]string{"-help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help: %v", err)
	}
	if _, err := parseLaunchOptions([]string{"-conversation", "unknown"}); err == nil {
		t.Fatal("invalid conversation accepted")
	}
}

func TestResolveLaunchExplicitValuesOverrideProfile(t *testing.T) {
	p := config.Profile{
		Version:      1,
		Provider:     config.Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "profile-model", Ownership: "external"},
		Generation:   config.Generation{MaxTokens: 256, ContextLimit: 4096, Temperature: .8},
		Conversation: &config.Conversation{Character: "profile.json", UserName: "Alex"},
	}
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := config.SaveCopy(p, path); err != nil {
		t.Fatal(err)
	}
	o, err := resolveLaunch(launchRequest{args: []string{"-chat", "-profile", path, "-model", "override", "-temperature=0", "-character-png", "", "-character-main-prompt=false"}})
	if err != nil {
		t.Fatal(err)
	}
	if o.model != "override" || o.temperature != 0 || o.characterPath != "" || o.preferCharacterPrompt {
		t.Fatal("explicit values lost to profile defaults")
	}
	if o.maxTokens != 256 || o.contextLimit != 4096 || o.userName != "Alex" {
		t.Fatal("missing values did not inherit profile settings")
	}
	if o.selectedProfile.Provider.Model != "override" || o.selectedProfile.Generation.Temperature != 0 || *o.selectedProfile.Conversation.PreferCharacterPrompt {
		t.Fatal("effective profile disagrees with launch options")
	}
}

func TestResolveLaunchClonesHandoffProfile(t *testing.T) {
	p := config.Profile{
		Version:      1,
		Provider:     config.Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local", Ownership: "external"},
		Generation:   config.Generation{MaxTokens: 256, ContextLimit: 4096},
		Conversation: &config.Conversation{UserName: "Alex"},
	}
	o, err := resolveLaunch(launchRequest{args: []string{"-profile", "missing.json", "-user-name", "Taylor"}, profile: &p})
	if err != nil {
		t.Fatal(err)
	}
	if o.userName != "Taylor" || p.Conversation.UserName != "Alex" {
		t.Fatal("handoff profile was not resolved independently")
	}
	if _, err := resolveLaunch(launchRequest{args: []string{"-check-profile"}}); err == nil {
		t.Fatal("check-profile accepted without profile")
	}
}
