package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/tui/settings"
)

type settingsClient struct {
	llm.Client
}

func TestApplyUneditedLaunchOverridePersistsCurrentProtocol(t *testing.T) {
	m := settingsModel(t)
	m.application.Temporary = true
	saved := m.application.Preferences
	profile := m.profile.Profile.Clone()
	profile.Provider.Kind = "kobold"
	saved.Profile = &profile
	if err := config.SavePreferences(m.application.PreferencePath, saved); err != nil {
		t.Fatal(err)
	}
	// The runtime and Settings show the explicit llama launch override.
	m.editSettings("model")
	m.formKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.modeChange == nil {
		t.Fatal("Apply silently ignored the visible launch settings")
	}
	if got := m.modeChange.Preferences.Profile.Provider.Kind; got != "llama" {
		t.Fatal("Apply used stale saved protocol", got)
	}
	// Mirror the application restart loop, then reload with no launch override.
	if err := config.SavePreferences(m.application.PreferencePath, m.modeChange.Preferences); err != nil {
		t.Fatal(err)
	}
	reloaded, err := config.LoadPreferences(m.application.PreferencePath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Profile.Provider.Kind != "llama" {
		t.Fatal("restart restored stale protocol")
	}
	file, err := config.Load(reloaded.ProfilePath)
	if err != nil || file.Provider.Kind != "llama" {
		t.Fatal("profile file disagrees with application settings", err)
	}
}

// A profile loaded from disk has nothing to diff but must still be
// committed, or loading a setup would silently do nothing.
func TestLoadedProfileIsCommittedOnApply(t *testing.T) {
	m := settingsModel(t)
	root := t.TempDir()
	p := m.profile.Profile.Clone()
	p.Identity.UserName = "Alex"
	path := filepath.Join(root, "custom.json")
	if err := config.SaveCopy(p, path); err != nil {
		t.Fatal(err)
	}
	m.editSettings("profiles")
	focusSetting(m, "load-profile")
	m.chrome.Dialog.Form.OpenPicker(m.settingsProps())
	picker := m.chrome.Dialog.Form.Picker
	m.Update(settings.ScanMsg{ID: picker.ID, Items: []string{path}})
	m.chrome.Dialog.Form.PickerKey(tea.KeyPressMsg{Code: tea.KeyEnter}, m.settingsProps())
	m.editSettings("generation")
	m.applyForm()
	if m.chrome.Dialog != nil {
		t.Fatalf("apply refused: %q", m.chrome.Dialog.Form.Error)
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
	profile := m.profile.Profile.Clone()
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
	if _, err := m.agent.Finish(false); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, path := range []string{custom, stateFileOf(m), m.application.PreferencePath} {
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
	m.chrome.Dialog.Form.Editor.SetValue("200")
	m.applyForm()
	if m.modeChange != nil {
		t.Fatal("a refused apply queued a restart")
	}
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form.Error == "" {
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
	m.chrome.Dialog.Form.Editor.SetValue("not-a-number")
	m.applyForm()
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form.Error == "" {
		t.Fatal("invalid candidate closed the form or applied silently")
	}
	if m.modeChange != nil {
		t.Fatal("invalid candidate queued a restart")
	}
}

// Applying settings restarts the launch so the profile is composed into
// fresh services. The queued restart must carry the draft, the history and
// the session directory, and must not have started a run.
func TestSettingsApplyQueuesRestartWithContextAndDraft(t *testing.T) {
	m := settingsModel(t)
	before, err := os.ReadFile(stateFileOf(m))
	if err != nil {
		t.Fatal(err)
	}
	m.editSettings("model")
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form.EditSession.Section != "model" {
		t.Fatal("model editor unavailable")
	}
	focusSetting(m, "model")
	m.chrome.Dialog.Form.Editor.SetValue("another-model")
	m.applyForm()
	if m.chrome.Dialog != nil {
		t.Fatalf("apply refused: %q", m.chrome.Dialog.Form.Error)
	}
	if m.modeChange == nil {
		t.Fatal("apply did not queue a restart")
	}
	if m.modeChange.Preferences.Profile.Provider.Model != "another-model" {
		t.Fatalf("model = %q", m.modeChange.Preferences.Profile.Provider.Model)
	}
	if m.modeChange.Draft != "unfinished draft" {
		t.Fatalf("draft = %q", m.modeChange.Draft)
	}
	if len(m.modeChange.History) != 3 {
		t.Fatalf("history carried %d messages, want 3", len(m.modeChange.History))
	}
	if !m.agent.Status().Idle {
		t.Fatal("apply started a run")
	}
	after, err := os.ReadFile(stateFileOf(m))
	if err != nil || string(after) != string(before) {
		t.Fatal("apply rewrote the saved task before the restart landed")
	}
}

// The Workspace page keeps staged values from another page in the same
// dialog, and always carries the session directory into the restart.
func TestWorkspacePageRetainsStagedModelAndSession(t *testing.T) {
	m := settingsModel(t)
	original := snapshotOf(m).Directory
	m.application.Preferences.Workspace = original
	m.editSettings("model")
	focusSetting(m, "model")
	m.chrome.Dialog.Form.Editor.SetValue("selected-model")
	m.editSettings("workspace")
	focusSetting(m, "app-workspace")
	m.chrome.Dialog.Form.Editor.SetValue(t.TempDir())
	m.applyForm()
	if m.chrome.Dialog != nil {
		t.Fatalf("apply refused: %q", m.chrome.Dialog.Form.Error)
	}
	if m.modeChange == nil {
		t.Fatal("apply did not queue a restart")
	}
	if m.modeChange.Preferences.Profile.Provider.Model != "selected-model" {
		t.Fatal("workspace page lost the staged model")
	}
	if m.modeChange.Directory != original {
		t.Fatalf("restart moved the session: %q -> %q", original, m.modeChange.Directory)
	}
}

// The workspace is resolved and checked, so the relaunch comes up rooted at
// a directory that exists rather than at whatever the editor string said.
func TestWorkspacePageRejectsAMissingWorkspace(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("workspace")
	focusSetting(m, "app-workspace")
	m.chrome.Dialog.Form.Editor.SetValue(filepath.Join(t.TempDir(), "does-not-exist"))
	m.applyForm()
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form.Error == "" {
		t.Fatal("a missing workspace was accepted")
	}
	if m.modeChange != nil {
		t.Fatal("a refused apply still queued a restart")
	}
}

// Opening a settings page and applying it without touching anything must
// not restart the launch: the reader did not ask for one.
func TestUnchangedSettingsDoNotRestart(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("model")
	m.applyForm()
	if m.modeChange != nil || m.chrome.Dialog == nil {
		t.Fatal("an unchanged draft restarted the launch")
	}
}
