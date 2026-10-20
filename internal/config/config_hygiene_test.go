package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func minimal() Profile {
	return Profile{
		Version:    1,
		Provider:   Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local"},
		Generation: Generation{MaxTokens: 256, ContextLimit: 4096},
	}
}

// A profile with no identity block must not acquire one because someone
// changed the model. It used to: ApplySection materialised Identity for every
// section, and a block created that way then persisted into the file.
func TestChangingTheModelDoesNotInventAnIdentity(t *testing.T) {
	p := minimal()
	if err := ApplySection(&p, SectionModel, map[string]string{
		"kind": "llama", "endpoint": "http://127.0.0.1:9999", "model": "other",
	}); err != nil {
		t.Fatal(err)
	}
	if p.Identity != nil {
		t.Fatalf("an identity block was invented: %+v", p.Identity)
	}
	if p.Provider.Model != "other" {
		t.Fatalf("model = %q", p.Provider.Model)
	}
}

// A section this function does not know is a typo. It used to return success
// after having already written to the profile.
func TestUnknownSectionIsRejectedAndWritesNothing(t *testing.T) {
	p := minimal()
	before := p
	err := ApplySection(&p, "generaton", map[string]string{"temperature": "0.5"})
	if err == nil {
		t.Fatal("a typo'd section was accepted")
	}
	if p.Identity != nil {
		t.Fatalf("a rejected section still mutated the profile: %+v", p.Identity)
	}
	if p.Generation.Temperature != before.Generation.Temperature {
		t.Fatalf("a rejected section still wrote: %v", p.Generation.Temperature)
	}
}

// The sections the editor offers and the sections ApplySection handles are
// one list. A page added to the menu that nothing applies would be a form
// that saves nothing.
func TestEveryOfferedSectionIsHandled(t *testing.T) {
	values := map[string]map[string]string{
		SectionModel:      {"kind": "llama", "endpoint": "http://127.0.0.1:8080", "model": "local"},
		SectionGeneration: {"temperature": "0.7", "max": "256", "context": "4096"},
		SectionPrompts:    {"system": "rules", "post": ""},
		SectionYou:        {"user": "Alex", "persona": "", "name": "GEKTOR"},
	}
	for _, section := range Sections() {
		p := minimal()
		if err := ApplySection(&p, section, values[section]); err != nil {
			t.Errorf("section %q is offered but not handled: %v", section, err)
		}
	}
}

// Plain Unmarshal drops keys it does not recognise, so a renamed field would
// quietly reset every user who had set it. Profile.Load already refuses them;
// preferences must too.
func TestUnknownPreferenceKeyIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"them":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPreferences(path); err == nil {
		t.Fatal("an unknown key was silently ignored")
	} else if !strings.Contains(err.Error(), "them") {
		t.Fatalf("the error does not name the offending key: %v", err)
	}
}

func TestPreferencesRoundTripKeepsTheHarness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.json")
	want := Preferences{
		Version: 1,
		Theme:   "light",
		Harness: Harness{MaxSteps: 42},
	}
	if err := SavePreferences(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPreferences(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Harness != want.Harness {
		t.Fatalf("harness came back as %+v, want %+v", got.Harness, want.Harness)
	}
	if got.Theme != want.Theme {
		t.Fatalf("theme came back as %q", got.Theme)
	}
}

func TestRetiredHarnessFieldsDropOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"harness":{"max_steps":42,"nudges":"recovery","think_budget":6000}}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPreferences(path)
	if err != nil || p.Harness.MaxSteps != 42 {
		t.Fatalf("prefs=%+v err=%v", p, err)
	}
	if err := SavePreferences(path, p); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "nudges") || strings.Contains(string(data), "think_budget") {
		t.Fatalf("retired fields retained: %s", data)
	}
}

// The nudge vocabulary has one declaration, in internal/harness, which both
// the loop and this validator read. They used to keep separate lists, so a
// rename had to be made twice and the validator could accept a value the loop
// then rejected.
