package settings

import (
	"testing"

	"github.com/keshon/gektor/internal/config"
)

// Every category the rail can show must carry its own hint. Generation
// used to fall through to the generic fallback, so the page described
// nothing specific about itself.
func TestEveryCategoryHasAHint(t *testing.T) {
	generic := "Changes apply to this session; history stays intact."
	for _, c := range categories {
		if got := Hint(c.ID); got == "" || got == generic {
			t.Fatalf("category %q has no hint of its own: %q", c.ID, got)
		}
	}
}

// The Application page offers the workspace and nothing else. A page that
// emitted a key nothing reads would let a draft stage values that silently
// vanish on apply.
func TestApplicationPageEmitsOnlyTheWorkspace(t *testing.T) {
	_, fields := PageFields(config.Profile{Version: 1}, "application", config.Preferences{Workspace: "D:/work"})
	want := []string{"app-workspace"}
	if len(fields) != len(want) {
		t.Fatalf("fields = %d, want %d", len(fields), len(want))
	}
	for i, key := range want {
		if fields[i].Key != key {
			t.Fatalf("field %d = %q, want %q", i, fields[i].Key, key)
		}
	}
	if fields[0].Value != "D:/work" {
		t.Fatalf("workspace value = %q", fields[0].Value)
	}
}

// PageFields normalizes a nil Identity before building pages. The
// Editor fields must not alias the profile they were built from: a form
// value that reached back into the live profile would edit it in place.
func TestPageFieldsDoNotAliasTheProfile(t *testing.T) {
	p := config.Profile{
		Version:    1,
		Provider:   config.Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local"},
		Generation: config.Generation{MaxTokens: 100, ContextLimit: 1000},
		Identity:   &config.Identity{UserName: "User"},
	}
	_, fields := PageFields(p, "you", config.Preferences{})
	fields[0].Value = "mutated"
	if p.Identity.UserName != "User" {
		t.Fatalf("Identity aliased: %+v", p.Identity)
	}
}
