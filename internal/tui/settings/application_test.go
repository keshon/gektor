package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/config"
)

func TestPreferencesSkipAnUnchangedWrite(t *testing.T) {
	dir := t.TempDir()
	m := &Application{}
	path := filepath.Join(dir, "application.json")
	*m = Application{
		PreferencePath: path,
		Preferences:    config.Preferences{Version: 1},
	}

	if err := m.Save(nil, "act", config.Appearance{}); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 {
		t.Fatal("first write did not record what it stored")
	}
	// A repeat must succeed even when any attempted write would fail.
	m.PreferencePath = filepath.Join(path, "blocked")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := m.Save(nil, "act", config.Appearance{}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an identical payload was written again")
	}

	m.PreferencePath = path
	m.Preferences.Theme = "light"
	if err := m.Save(nil, "act", config.Appearance{}); err != nil {
		t.Fatal(err)
	}
	changed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(changed) == string(first) {
		t.Fatal("a real change was skipped")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
