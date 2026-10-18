package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Decoding is strict, which makes it a trap for the profiles this repository
// ships: nothing exercises them at build time, so a field deleted from
// Provider leaves the bundled default failing only on a user's machine, at
// first launch. Load every one of them.
func TestBundledProfilesLoad(t *testing.T) {
	dir := filepath.Join("..", "..", "profiles")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("bundled profiles unreadable: %v", err)
	}
	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if _, err := Load(path); err != nil {
			t.Errorf("%s does not load: %v", entry.Name(), err)
		}
		loaded++
	}
	if loaded == 0 {
		t.Fatal("no bundled profiles found; the check passed vacuously")
	}
}

func valid() Profile {
	return Profile{
		Version:    1,
		Provider:   Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local"},
		Generation: Generation{MaxTokens: 256, ContextLimit: 4096},
	}
}

// Decoding is strict on purpose. A misspelled setting must fail at load
// rather than being silently dropped and taking its effect with it: this is
// the only place a typo becomes visible.
func TestMisspelledFieldIsRejected(t *testing.T) {
	body := `{
      "version": 1,
      "provider": {"kind":"llama","url":"http://127.0.0.1:8080","model":"local"},
      "generation": {"max_tokens":256,"context_limit":4096},
      "identty": {"user_name":"Alex"}
    }`
	path := filepath.Join(t.TempDir(), "typo.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("a misspelled field was accepted")
	}
	if !strings.Contains(err.Error(), "identty") {
		t.Fatalf("error does not name the offending key: %v", err)
	}
}

// A profile written against a removed schema is rejected, not rewritten.
// Rewriting would mean keeping a registry of dead keys forever; failing
// tells the reader exactly which key to delete.
func TestRemovedKeysAreRejectedByName(t *testing.T) {
	for key, body := range map[string]string{
		"conversation": `{"version":1,"provider":{"kind":"llama","url":"http://127.0.0.1:8080","model":"local"},"generation":{"max_tokens":256,"context_limit":4096},"conversation":{"user_name":"Alex"}}`,
		"speech":       `{"version":1,"provider":{"kind":"llama","url":"http://127.0.0.1:8080","model":"local"},"generation":{"max_tokens":256,"context_limit":4096},"speech":{"max_seconds":120}}`,
	} {
		t.Run(key, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "old.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatalf("a removed %q block was accepted", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Fatalf("error does not name %q: %v", key, err)
			}
		})
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
