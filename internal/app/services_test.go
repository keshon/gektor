package app

import (
	"strings"
	"testing"

	"github.com/keshon/tars/internal/config"
)

// A composed launch owns its profile copy: the caller may keep mutating the
// struct it passed in without reaching into the running session's view of it.
func TestConfigureAgentOwnsItsProfile(t *testing.T) {
	p := config.Profile{
		Version:    1,
		Provider:   config.Provider{Kind: "llama", URL: "http://localhost:8080", Model: "local"},
		Generation: config.Generation{MaxTokens: 100, ContextLimit: 1000},
		Identity:   &config.Identity{UserName: "User", SystemPrompt: "base"},
	}
	services, err := ConfigureAgent(p, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p.Identity.UserName = "Changed outside"
	if services.Profile.Identity.UserName != "User" {
		t.Fatal("configuration shares the caller-owned profile")
	}
	// The identity block is deep-copied, not aliased.
	if services.Profile.Identity == p.Identity {
		t.Fatal("identity block is aliased, not copied")
	}
	if services.Identity.System != "base" {
		t.Fatalf("compiled identity = %+v", services.Identity)
	}
	if services.Identity.Name != "Assistant" {
		t.Fatalf("default assistant name = %q", services.Identity.Name)
	}
}

// A profile with no identity block is the ordinary case, not an edge case:
// the bundled agent profile ships without one. It must compile with
// defaults rather than producing an empty persona.
func TestConfigureAgentDefaultsAnAbsentIdentity(t *testing.T) {
	p := config.Profile{
		Version:    1,
		Provider:   config.Provider{Kind: "llama", URL: "http://localhost:8080", Model: "local"},
		Generation: config.Generation{MaxTokens: 100, ContextLimit: 1000},
	}
	if p.Identity != nil {
		t.Fatal("precondition: profile should have no identity block")
	}
	services, err := ConfigureAgent(p, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if services.Identity.System != config.DefaultSystemPrompt {
		t.Fatalf("default system prompt = %q", services.Identity.System)
	}
	if len(services.Warnings) != 0 {
		t.Fatalf("default identity warned: %v", services.Warnings)
	}
}

// A blank user name is refused rather than compiled into a prompt that
// addresses nobody.
func TestCompileRefusesABlankUserName(t *testing.T) {
	if _, _, err := Compile(IdentityOptions{User: "  ", System: "base"}); err == nil {
		t.Fatal("blank user name accepted")
	}
}

// The preset is interpolated into the identity, and an unexpanded macro is
// reported rather than shipped as literal text to the model.
func TestCompileFillsAndReportsMacros(t *testing.T) {
	id, warnings, err := Compile(IdentityOptions{Preset: "coding", Name: "Ada", User: "Alex", Persona: "a colleague", System: "base"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(id.System, "Response style (Coding)") {
		t.Fatalf("preset missing from identity: %q", id.System)
	}
	if !strings.Contains(id.System, "a colleague") {
		t.Fatalf("persona missing from identity: %q", id.System)
	}
	if len(warnings) != 0 {
		t.Fatalf("clean preset warned: %v", warnings)
	}
	if _, warnings, err := Compile(IdentityOptions{User: "Alex", System: "address {{user}} directly"}); err != nil || len(warnings) == 0 {
		t.Fatalf("unfilled macro went unreported: %v %v", warnings, err)
	}
}

// IdentityPrompt is only the profile's contribution. The agent's own
// operational instructions are added separately and must not appear here.
func TestIdentityPromptOmitsOperationalInstructions(t *testing.T) {
	got := IdentityPrompt(Identity{System: "be terse", PostHistory: "cite sources"})
	if !strings.Contains(got, "be terse") || !strings.Contains(got, "cite sources") {
		t.Fatalf("identity prompt lost its parts: %q", got)
	}
	if strings.Contains(got, "You are a local coding assistant") {
		t.Fatal("identity prompt duplicated the agent's own instructions")
	}
}