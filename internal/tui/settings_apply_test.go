package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/app"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
)

// A startup failure must surface rather than starting a program. The
// failure is forced with a SessionDirectory that is a file rather than a
// directory: Run writes the task state before it starts the program, so
// this returns an error without ever reaching the terminal.
func TestRunFailsOnUnwritableSessionDirectory(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Services: &app.Services{Client: &settingsClient{}, Directory: t.TempDir()}, SessionDirectory: blocker}
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Fatal("unwritable session directory accepted")
	}
}

type settingsClient struct {
	llm.Client
}

// A profile loaded from disk has nothing to diff but must still be
// committed, or loading a setup would silently do nothing.
func TestLoadedProfileIsCommittedOnApply(t *testing.T) {
	m := settingsModel(t)
	root := t.TempDir()
	p := m.config.Profile.Clone()
	p.Identity.UserName = "Alex"
	path := filepath.Join(root, "custom.json")
	if err := config.SaveCopy(p, path); err != nil {
		t.Fatal(err)
	}
	m.editSettings("profiles")
	focusSetting(m, "load-profile")
	m.dialog.form.OpenPicker(m.settingsProps())
	picker := m.dialog.form.Picker
	m.acceptSettingsScan(settingsScanMsg{ID: picker.ID, Items: []string{path}})
	m.dialog.form.PickerKey(tea.KeyPressMsg{Code: tea.KeyEnter}, m.settingsProps())
	m.editSettings("generation")
	m.applyForm()
	if m.dialog != nil {
		t.Fatalf("apply refused: %q", m.dialog.form.Error)
	}
	if m.modeChange == nil {
		t.Fatal("apply did not queue a restart")
	}
	if got := m.modeChange.Preferences.Profile.Identity.UserName; got != "Alex" {
		t.Fatalf("loaded profile was not committed: %q", got)
	}
	if m.modeChange.Preferences.ProfilePath != path {
		t.Fatalf("restart would not reload the loaded file: %q", m.modeChange.Preferences.ProfilePath)
	}
}

// A refused apply must leave everything on disk untouched: no custom
// profile, no preferences, and no queued restart.
func TestRefusedApplyLeavesFilesAndRuntimeAlone(t *testing.T) {
	m := settingsModel(t)
	root := t.TempDir()
	profile := m.config.Profile.Clone()
	custom := filepath.Join(root, "profiles", "custom", "custom-test.json")
	if err := os.MkdirAll(filepath.Dir(custom), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveCopy(profile, custom); err != nil {
		t.Fatal(err)
	}
	m.application.PreferencePath = filepath.Join(root, "var", "application.json")
	if err := os.MkdirAll(filepath.Dir(m.application.PreferencePath), 0o755); err != nil {
		t.Fatal(err)
	}
	m.application.Preferences.ProfilePath = custom
	m.application.Preferences.Profile = &profile
	if err := m.savePreferences(); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, path := range []string{custom, m.stateFile, m.application.PreferencePath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("fixture missing %s: %v", path, err)
		}
		before[path] = data
	}
	// A file where the custom profile directory belongs makes the custom
	// save fail late, after the candidate has been composed and validated.
	// requestProfileChange never reaches the preferences write, so blocking
	// the preferences path would pass without exercising anything.
	customDir := filepath.Dir(custom)
	if err := os.RemoveAll(customDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(customDir, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	delete(before, custom)
	m.editSettings("generation")
	focusSetting(m, "max")
	m.dialog.form.Editor.SetValue("200")
	m.applyForm()
	if m.modeChange != nil {
		t.Fatal("a refused apply queued a restart")
	}
	if m.dialog == nil || m.dialog.form.Error == "" {
		t.Fatal("a refused apply closed the form without reporting the failure")
	}
	for path, want := range before {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(want) {
			t.Fatalf("%s was rewritten by a refused apply", path)
		}
	}
	if info, err := os.Stat(customDir); err != nil || info.IsDir() {
		t.Fatal("a refused apply moved the custom directory aside instead of failing")
	}
}

// Apply validates before it writes. An unparseable response budget leaves
// the draft open with the error shown, rather than closing the form and
// taking the staged value with it.
func TestInvalidCandidateKeepsTheDraftOpen(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("generation")
	focusSetting(m, "temperature")
	m.dialog.form.Editor.SetValue("not-a-number")
	m.applyForm()
	if m.dialog == nil || m.dialog.form.Error == "" {
		t.Fatal("invalid candidate closed the form or applied silently")
	}
	if m.modeChange != nil {
		t.Fatal("invalid candidate queued a restart")
	}
}
