package config

import (
	"path/filepath"
	"testing"
)

func TestPreferencesPersistModesAndReplaceAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "var", "application.json")
	p, err := LoadPreferences(path)
	if err != nil || p.Mode != "chat" || p.Voice != "off" {
		t.Fatal(p, err)
	}
	p.Mode = "agent"
	p.Voice = "live"
	p.Workspace = "D:/work"
	if err := SavePreferences(path, p); err != nil {
		t.Fatal(err)
	}
	p.Voice = "off"
	if err := SavePreferences(path, p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPreferences(path)
	if err != nil || got.Mode != "agent" || got.Voice != "off" || got.Workspace != p.Workspace {
		t.Fatal(got, err)
	}
}
