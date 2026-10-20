package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/roles"
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
	// The relaunched environment must render and enforce that same value.
	reopened := &model{baseEnv: roles.Env{MaxSteps: saved.Harness.MaxSteps}, maxSteps: envMaxSteps(roles.Env{MaxSteps: saved.Harness.MaxSteps})}
	if reopened.stepBudget() != "∞" || reopened.sessionEnv().MaxSteps != unlimitedSteps {
		t.Fatal("reload lost the unlimited budget")
	}
}

func TestModeSelectedWithoutAnActiveSessionIsGlobal(t *testing.T) {
	m, path := harnessModel(t)
	m.plan = true
	m.command("/mode act")
	saved, err := config.LoadPreferences(path)
	if err != nil || saved.Mode != "act" {
		t.Fatalf("mode was not saved: %+v %v", saved, err)
	}
	m.stateFile = filepath.Join(t.TempDir(), "state.json")
	m.newTask()
	if m.plan {
		t.Fatal("new task reset mode")
	}
}

func TestFiniteStepLimitReplacesRestoredUnlimitedImmediately(t *testing.T) {
	m, _ := harnessModel(t)
	m.maxSteps, m.maxStepsOverride = unlimitedSteps, noStepLimit
	m.command("/max-steps 10")
	if m.stepBudget() != "10" || m.sessionEnv().MaxSteps != 10 {
		t.Fatal("header retained unlimited after selecting a finite limit")
	}
}

func TestTemporaryLaunchSavesExplicitWorkflowWithoutReplacingProfile(t *testing.T) {
	m, path := harnessModel(t)
	p := config.DefaultProfile()
	saved := config.Preferences{Version: 1, Mode: "plan", Profile: &p, Workspace: "installed"}
	if err := config.SavePreferences(path, saved); err != nil {
		t.Fatal(err)
	}
	m.application.Temporary = true
	m.application.Preferences.Workspace = "temporary"
	m.plan = true
	m.command("/mode act")
	m.command("/max-steps 0")
	got, err := config.LoadPreferences(path)
	if err != nil || got.Mode != "act" || got.Harness.MaxSteps != unlimitedSteps || got.Workspace != "installed" || got.Profile == nil || got.Profile.Provider != p.Provider {
		t.Fatalf("temporary launch lost workflow or replaced settings: %+v %v", got, err)
	}
}

func TestFailedGlobalModeSaveKeepsExecutingSessionAndMetadata(t *testing.T) {
	m, _ := sessionModel(t)
	m.plan = true
	dir := t.TempDir()
	m.stateFile = filepath.Join(dir, "state.json")
	if err := writeSavedMode(m.stateFile, true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "application.json")
	if err := os.WriteFile(path, []byte("invalid json"), 0600); err != nil {
		t.Fatal(err)
	}
	m.application = &Application{Temporary: true, PreferencePath: path, Preferences: config.Preferences{Version: 1, Mode: "plan"}}
	previous := m.sess
	m.command("/mode act")
	if !m.plan || m.sess != previous || readSavedMode(dir) != "Plan" {
		t.Fatal("failed persistence changed the executing mode or metadata")
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
