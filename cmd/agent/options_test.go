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

// The generated profile must be usable for both modes. ConfigureAgent
// substitutes a Conversation when one is missing but never writes it back,
// so switching a generated session to Chat was refused with a requirement
// nothing on that screen could satisfy.
func TestGeneratedProfileCarriesAConversation(t *testing.T) {
	o, err := parseLaunchOptions([]string{"-tui", "-backend-kind", "llama", "-backend", "http://127.0.0.1:8080"})
	if err != nil {
		t.Fatal(err)
	}
	p := launchProfile(resolvedLaunch{launchOptions: o}, 32768)
	if p.Conversation == nil {
		t.Fatal("generated profile has no conversation settings")
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("generated profile rejected: %v", err)
	}
	if p.Conversation.UserName != config.DefaultUserName || p.Conversation.SystemPrompt != config.DefaultSystemPrompt {
		t.Fatalf("conversation defaults: %+v", p.Conversation)
	}
	// Characters belong to Chat. An Agent must not be handed one.
	if p.Conversation.Character != "" {
		t.Fatalf("generated Agent profile carries a character: %q", p.Conversation.Character)
	}
}

// The launch docs/tui.md shows has no -profile, and it still has to be
// configurable: without a profile the services handle and the Application
// both came back nil, which disabled every Settings, Mode and Voice row
// with nothing on screen saying why.
func TestLaunchWithoutProfileStillValid(t *testing.T) {
	o, err := resolveLaunch(launchRequest{args: []string{"-tui", "-backend-kind", "llama", "-backend", "http://127.0.0.1:8080", "-workspace", "."}})
	if err != nil {
		t.Fatal(err)
	}
	// The flag defaults alone must pass validation, or the fix would turn
	// a greyed menu into a startup failure.
	p := launchProfile(o, 32768)
	if err := p.Validate(); err != nil {
		t.Fatalf("generated profile rejected: %v", err)
	}
	if p.Version != 1 || p.Provider.Kind != "llama" || p.Provider.URL != "http://127.0.0.1:8080" || p.Provider.Model != o.model {
		t.Fatalf("generated profile lost the launch flags: %+v", p)
	}
	if p.Generation.MaxTokens != o.maxTokens || p.Generation.ContextLimit != 32768 {
		t.Fatalf("generated generation: %+v", p.Generation)
	}
	if applicationControl(launchRequest{}, p, "", false, ".") == nil {
		t.Fatal("generated profile must still yield an Application")
	}
}

// Validate rejects a context limit that does not exceed the response
// budget. The probe returns 0 when a backend will not report its window,
// and the launch still has to produce a usable profile.
func TestLaunchProfileContextFloor(t *testing.T) {
	o, err := parseLaunchOptions([]string{"-tui", "-backend-kind", "llama", "-backend", "http://127.0.0.1:8080"})
	if err != nil {
		t.Fatal(err)
	}
	for _, probed := range []int{0, 1000, 65536} {
		p := launchProfile(resolvedLaunch{launchOptions: o}, probed)
		if err := p.Validate(); err != nil {
			t.Fatalf("probed %d: generated profile rejected: %v", probed, err)
		}
		if probed > o.maxTokens && p.Generation.ContextLimit != probed {
			t.Fatalf("probed %d: reported window replaced by %d", probed, p.Generation.ContextLimit)
		}
	}
}