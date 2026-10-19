package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/config"
)

// These three used to be flags and live model fields, so a step budget or a
// nudge level set in the TUI was still on screen when the process closed and
// gone by the next launch. The only way to keep one was to pass it on the
// command line every time.

func harnessModel(t *testing.T) (*model, string) {
	t.Helper()
	m := sizeModel(t, testModel())
	root := t.TempDir()
	path := filepath.Join(root, "application.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	m.application = &Application{
		PreferencePath: path,
		Preferences:    config.Preferences{Version: 1},
	}
	return m, path
}

func TestMaxStepsSurvivesAReload(t *testing.T) {
	m, path := harnessModel(t)
	m.command("/max-steps 42")
	if m.maxStepsOverride != 42 {
		t.Fatalf("not applied: %+v", m.maxStepsOverride)
	}
	saved, err := config.LoadPreferences(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Harness.MaxSteps != 42 {
		t.Fatalf("saved %d, want 42", saved.Harness.MaxSteps)
	}
}

// "No limit" has to come back as no limit. Persisting it as zero would read
// back as "unset" and land on the launch default, which is the bug this
// whole change is about, one field over.
func TestNoStepLimitSurvivesAsNoStepLimit(t *testing.T) {
	m, path := harnessModel(t)
	m.command("/max-steps 0")
	if m.maxStepsOverride != noStepLimit {
		t.Fatalf("not applied: %+v", m.maxStepsOverride)
	}
	saved, err := config.LoadPreferences(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Harness.MaxSteps != unlimitedSteps {
		t.Fatalf("saved %d, want %d", saved.Harness.MaxSteps, unlimitedSteps)
	}
	if saved.Harness.MaxSteps == 0 {
		t.Fatal("no limit was saved as unset, which reads back as the default")
	}
}

func TestHarnessWritesAreIndependentAndIdempotent(t *testing.T) {
	m, path := harnessModel(t)
	m.command("/max-steps 7")

	saved, err := config.LoadPreferences(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Harness.MaxSteps != 7 {
		t.Fatalf("lost one: %+v", saved.Harness)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.saveHarness(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an identical harness was written again")
	}
}

// A preferences file written before harness settings existed must still load
// and still validate: absent means the launch default.
func TestPreferencesWithoutHarnessStillLoad(t *testing.T) {
	_, path := harnessModel(t)
	if err := os.WriteFile(path, []byte(`{"version":1,"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := config.LoadPreferences(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Harness != (config.Harness{}) {
		t.Fatalf("absent harness became %+v", p.Harness)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	_ = tea.KeyEnter
}
