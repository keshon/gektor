package settings

import (
	"testing"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/speech"
)

// PageFields normalizes a nil Conversation eleven lines before it
// dereferenced a nil Speech. Every gate that normally keeps a speech-less
// profile off this page is a category filter or a menu condition, so any
// caller reaching the page directly turned into a crash.
// Every category the rail can show must carry its own hint. Generation
// fell through to the generic fallback, so the page described nothing
// specific about itself.
func TestEveryCategoryHasAHint(t *testing.T) {
	generic := "Changes apply to this session; history stays intact."
	for _, c := range categories {
		if c.ID == "character" {
			continue // Chat only; not reachable from an Agent rail
		}
		if got := Hint(c.ID); got == "" || got == generic {
			t.Fatalf("category %q has no hint of its own: %q", c.ID, got)
		}
	}
}

// app-profile was handled in the picker and the catalog but produced by no
// field, so the Application page could never actually load a profile that
// way.
func TestNoFieldProducesTheAppProfileKey(t *testing.T) {
	for _, section := range []string{"application", "profiles", "model", "speech", "prompts", "you", "generation"} {
		_, fields := PageFields(config.Profile{Version: 1}, section, config.Preferences{}, "off")
		for _, f := range fields {
			if f.Key == "app-profile" {
				t.Fatalf("section %q emits the unreachable app-profile key", section)
			}
		}
	}
}

func TestSpeechPageSurvivesAProfileWithoutSpeech(t *testing.T) {
	p := config.Profile{
		Version:    1,
		Provider:   config.Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local", Ownership: "external"},
		Generation: config.Generation{MaxTokens: 100, ContextLimit: 1000},
	}
	if p.Speech != nil {
		t.Fatal("precondition: profile should have no speech")
	}
	title, fields := PageFields(p, "speech", config.Preferences{}, "off")
	if title != "Speech services" {
		t.Fatalf("title = %q", title)
	}
	want := []string{"microphone", "variant", "speech-model", "seconds", "vad", "mic-start", "mic-end", "mic-threshold"}
	if len(fields) != len(want) {
		t.Fatalf("fields = %d, want %d", len(fields), len(want))
	}
	for i, key := range want {
		if fields[i].Key != key {
			t.Fatalf("field %d = %q, want %q", i, fields[i].Key, key)
		}
	}
	if fields[4].Value != "silero" {
		t.Fatalf("default detector = %q", fields[4].Value)
	}
}

// The editor fields must not alias the profile they were built from: a
// form value that reaches back into the live profile edits it in place.
func TestPageFieldsDoNotAliasTheProfile(t *testing.T) {
	speechCfg := &speech.Config{Microphone: "mic", MaxSeconds: 120}
	p := config.Profile{
		Version:      1,
		Provider:     config.Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local", Ownership: "external"},
		Generation:   config.Generation{MaxTokens: 100, ContextLimit: 1000},
		Conversation: &config.Conversation{UserName: "User"},
		Speech:       speechCfg,
	}
	_, fields := PageFields(p, "speech", config.Preferences{}, "off")
	fields[0].Value = "mutated"
	if speechCfg.Microphone == "mutated" {
		t.Fatal("an editor field wrote through to the live speech config")
	}
	if p.Conversation.UserName != "User" {
		t.Fatalf("conversation aliased: %+v", p.Conversation)
	}
}