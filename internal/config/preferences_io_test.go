package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnknownPreferenceKeyIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.json")
	err := os.WriteFile(path, []byte(`{"version":1,"them":"dark"}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	_, err = LoadPreferences(path)
	if err == nil {
		t.Fatal("an unknown key was silently ignored")
	}
	if !strings.Contains(err.Error(), "them") {
		t.Fatalf("the error does not name the offending key: %v", err)
	}
}

func TestPreferencesRoundTripKeepsTheHarness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.json")
	want := Preferences{
		Version: 1,
		Theme:   "light",
		Harness: Harness{MaxSteps: 42, Steering: true},
	}

	err := SavePreferences(path, want)
	if err != nil {
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

func TestRetiredHarnessFieldsAreRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.json")
	for _, body := range []string{`{"version":1,"harness":{"nudges":"recovery"}}`, `{"version":1,"harness":{"think_budget":6000}}`, `{"version":1,"appearance":{"density":"compact"}}`} {
		err := os.WriteFile(path, []byte(body), 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = LoadPreferences(path)
		if err == nil {
			t.Fatal("accepted retired preferences", body)
		}
	}
}

func TestPreferencesRejectExtraDocumentsAndOversizedInput(t *testing.T) {
	const document = `{"version":1,"theme":"dark"}`
	cases := map[string]string{
		"second document":              document + `{}`,
		"trailing garbage":             document + `invalid`,
		"oversized valid prefix":       document + strings.Repeat(" ", 1<<20),
		"hidden document beyond limit": document + strings.Repeat(" ", 1<<20) + `{}`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "application.json")
			err := os.WriteFile(path, []byte(body), 0o600)
			if err != nil {
				t.Fatal(err)
			}

			_, err = LoadPreferences(path)
			if err == nil {
				t.Fatal("accepted invalid preferences input")
			}
		})
	}
}

func TestMissingPreferencesUseDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	preferences, err := LoadPreferences(path)
	if err != nil {
		t.Fatal(err)
	}

	if preferences != (Preferences{Version: 1}) {
		t.Fatalf("missing-file defaults = %+v", preferences)
	}
}

func TestInvalidPreferencesKeepSavedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.json")
	original := []byte(`{"version":1,"theme":"dark"}`)
	err := os.WriteFile(path, original, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	preferences := Preferences{Version: 1, Theme: "unknown"}
	err = SavePreferences(path, preferences)
	if err == nil {
		t.Fatal("saved invalid preferences")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("saved preferences changed: %s", got)
	}
}
