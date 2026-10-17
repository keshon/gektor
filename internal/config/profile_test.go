package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func valid() Profile {
	return Profile{
		Version:    1,
		Provider:   Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local", Ownership: "external"},
		Generation: Generation{MaxTokens: 256, ContextLimit: 4096},
	}
}

// A profile saved before the rename carries the identity under the old key.
// Discarding it would silently erase a reader's persona the first time they
// loaded their own setup, so the block is moved rather than dropped.
func TestLegacyConversationBlockBecomesIdentity(t *testing.T) {
	body := `{
      "version": 1,
      "provider": {"kind":"llama","url":"http://127.0.0.1:8080","model":"local","ownership":"external"},
      "generation": {"max_tokens":256,"context_limit":4096},
      "conversation": {"user_name":"Alex","system_prompt":"be terse","name":"Ada","prompt_preset":"coding"}
    }`
	path := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Identity == nil {
		t.Fatal("legacy conversation block was discarded")
	}
	if p.Identity.UserName != "Alex" || p.Identity.SystemPrompt != "be terse" || p.Identity.Name != "Ada" || p.Identity.PromptPreset != "coding" {
		t.Fatalf("identity not carried over: %+v", p.Identity)
	}
}

// The speech block has no successor. It is dropped rather than mapped, so a
// stale profile does not keep looking configured for a service that is gone.
func TestLegacySpeechBlockIsDropped(t *testing.T) {
	body := `{
      "version": 1,
      "provider": {"kind":"llama","url":"http://127.0.0.1:8080","model":"local","ownership":"external"},
      "generation": {"max_tokens":256,"context_limit":4096},
      "speech": {"microphone":"mic","max_seconds":120}
    }`
	path := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("legacy speech block broke the load: %v", err)
	}
}

// Carrying both is a genuine conflict with no safe resolution: the reader
// has two identity blocks and no way to say which one they meant.
func TestBothIdentityBlocksIsAnError(t *testing.T) {
	body := `{
      "version": 1,
      "provider": {"kind":"llama","url":"http://127.0.0.1:8080","model":"local","ownership":"external"},
      "generation": {"max_tokens":256,"context_limit":4096},
      "conversation": {"user_name":"Alex"},
      "identity": {"user_name":"Sam"}
    }`
	path := filepath.Join(t.TempDir(), "both.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "both present") {
		t.Fatalf("conflicting identity blocks accepted: %v", err)
	}
}

// Typo protection is the reason the loader is strict, and the migration
// must not weaken it for every other key.
func TestMisspelledFieldsAreStillRejected(t *testing.T) {
	body := `{
      "version": 1,
      "provider": {"kind":"llama","url":"http://127.0.0.1:8080","model":"local","ownership":"external"},
      "generation": {"max_tokens":256,"context_limit":4096},
      "identty": {"user_name":"Alex"}
    }`
	path := filepath.Join(t.TempDir(), "typo.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("a misspelled field was accepted")
	}
}

// A save followed by a load has to round-trip, or every settings apply
// would quietly lose settings the next time the profile was reopened.
func TestIdentityRoundTrips(t *testing.T) {
	p := valid()
	p.Identity = &Identity{UserName: "Alex", UserPersona: "a colleague", SystemPrompt: "be terse", PostHistory: "cite sources", Name: "Ada", PromptPreset: "coding"}
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := SaveCopy(p, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Identity == nil || *loaded.Identity != *p.Identity {
		t.Fatalf("identity did not round-trip: %+v", loaded.Identity)
	}
}

// Clone must deep-copy, or an editor reaching into a staged profile edits
// the live session's copy in place.
func TestCloneCopiesIdentity(t *testing.T) {
	p := valid()
	p.Identity = &Identity{UserName: "User"}
	next := p.Clone()
	next.Identity.UserName = "Changed"
	if p.Identity.UserName != "User" {
		t.Fatal("Clone aliased the identity block")
	}
}

// A section that supplies an identity has to be able to create one; the
// bundled profile has none at all.
func TestApplySectionCreatesTheIdentityItEdits(t *testing.T) {
	p := valid()
	if p.Identity != nil {
		t.Fatal("precondition: profile should have no identity block")
	}
	if err := ApplySection(&p, "you", map[string]string{"user": "Alex", "persona": "a colleague"}); err != nil {
		t.Fatal(err)
	}
	if p.Identity == nil || p.Identity.UserName != "Alex" || p.Identity.UserPersona != "a colleague" {
		t.Fatalf("identity not created: %+v", p.Identity)
	}
}