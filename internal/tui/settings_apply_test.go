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

// Run owns a fallback runtime only when the caller did not borrow one, and
// a startup failure must close the fallback without ever closing a
// borrowed runtime.
//
// The failure is forced with a SessionDirectory that is a file rather than
// a directory: Run writes the task state before it starts the program, so
// this returns an error without ever reaching the terminal.
func TestTUIRuntimeOwnershipOnStartupFailure(t *testing.T) {
	for _, borrowed := range []bool{false, true} {
		blocker := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		client := &settingsClient{}
		services := &app.Services{Client: client, Directory: t.TempDir()}
		cfg := Config{Services: services, SessionDirectory: blocker}
		if borrowed {
			cfg.Runtime = app.NewRuntime(services)
		}
		if _, err := Run(context.Background(), cfg); err == nil {
			t.Fatal("unwritable session directory accepted")
		}
		want := 1
		if borrowed {
			want = 0
		}
		if client.closed != want {
			t.Fatal("TUI closed a borrowed runtime or leaked its fallback", client.closed, borrowed)
		}
		if borrowed {
			cfg.Runtime.Close()
			if client.closed != 1 {
				t.Fatal("owner failed to close borrowed runtime")
			}
		}
	}
}

type settingsClient struct {
	llm.Client
	closed int
}

func (c *settingsClient) Close() { c.closed++ }

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
	m.openSettingsPicker()
	picker := m.dialog.form.Picker
	m.acceptSettingsScan(settingsScanMsg{ID: picker.ID, Items: []string{path}})
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
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
// profile, no session settings, and no queued restart.
func TestRefusedApplyLeavesFilesAndRuntimeAlone(t *testing.T) {
	m := settingsModel(t)
	root := t.TempDir()
	profile := m.config.Profile.Clone()
	profile.Provider.ModelFile = filepath.Join(root, "assets", "model.gguf")
	custom := filepath.Join(root, "profiles", "custom", "custom-test.json")
	if err := os.MkdirAll(filepath.Dir(custom), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveCopy(profile, custom); err != nil {
		t.Fatal(err)
	}
	m.application.PreferencePath = filepath.Join(root, "var", "application.json")
	m.application.Preferences.ProfilePath = custom
	m.application.Preferences.Profile = &profile
	if err := m.saveSessionSettings(); err != nil {
		t.Fatal(err)
	}
	sessionSettings := filepath.Join(m.sessionDirectory(), "settings.json")
	before := map[string][]byte{}
	for _, path := range []string{custom, m.stateFile, sessionSettings} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("fixture missing %s: %v", path, err)
		}
		before[path] = data
	}
	// A directory where the global preferences file belongs makes the
	// write fail late, after the candidate has been composed.
	if err := os.MkdirAll(m.application.PreferencePath, 0o700); err != nil {
		t.Fatal(err)
	}
	m.editSettings("generation")
	focusSetting(m, "max")
	m.dialog.form.Editor.SetValue("999999")
	m.applyForm()
	if m.modeChange != nil {
		t.Fatal("a refused apply queued a restart")
	}
	for path, want := range before {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(want) {
			t.Fatalf("%s was rewritten by a refused apply", path)
		}
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