package tui

import (
 "github.com/keshon/gektor/internal/tui/screen"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	fieldform "github.com/keshon/gektor/internal/tui/components/fields"
)

// Loading a profile stages it; it does not apply. The running session
// keeps its own profile until Apply, and then the restart carries the
// loaded values and the same session directory.
func TestLoadedProfileStagedUntilApply(t *testing.T) {
	m := settingsModel(t)
	m.application.Preferences.ProfilePath = "old.json"
	root := t.TempDir()
	p := m.profile.Profile.Clone()
	p.Identity.UserName = "Alex"
	p.Generation.Temperature = .8
	path := filepath.Join(root, "custom.json")
	if err := config.SaveCopy(p, path); err != nil {
		t.Fatal(err)
	}
	id := stateFileOf(m)
	m.editSettings("profiles")
	focusSetting(m, "load-profile")
	m.chrome.OpenFormPicker(m.chrome.DialogID())
	picker := formSnapshot(m).Picker
	m.Update(fieldform.ScanMsg{ID: picker.ID, Items: []string{path}})
	m.chrome.FormKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.profile.Profile.Identity.UserName != "User" {
		t.Fatal("load applied without Apply")
	}
	m.editSettings("generation")
	if formSnapshot(m).EditorValue != "0.8" {
		t.Fatal("loaded settings not shown")
	}
	m.applyForm()
	if m.chrome.DialogInfo().Kind != screen.NoDialog {
		t.Fatalf("apply refused: %q", formSnapshot(m).Error)
	}
	if m.modeChange == nil || stateFileOf(m) != id {
		t.Fatal("profile load lost the session")
	}
	if got := m.modeChange.Preferences.Profile.Identity.UserName; got != "Alex" {
		t.Fatalf("loaded profile was not applied: %q", got)
	}
	if m.modeChange.Preferences.ProfilePath != path {
		t.Fatalf("loaded source was not carried: %q", m.modeChange.Preferences.ProfilePath)
	}
}

// A built-in profile must never be written to. Apply saves a copy under
// profiles/custom and hands that path to the restart, then reuses it for
// every later apply instead of deriving a new one.
func TestApplyAutomaticallySavesReusableCustomProfile(t *testing.T) {
	m := settingsModel(t)
	root := t.TempDir()
	original := m.profile.Profile.Clone()
	builtin := filepath.Join(root, "profiles", "builtin.json")
	if err := config.SaveCopy(original, builtin); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(builtin)
	*m.application = Application{PreferencePath: filepath.Join(root, "var", "application.json"), Preferences: config.Preferences{Version: 1, ProfilePath: builtin, Profile: &original}}
	m.editSettings("you")
	replaceFormText(m, "Alex")
	m.applyForm()
	if m.chrome.DialogInfo().Kind != screen.NoDialog {
		t.Fatal("apply did not finish", formSnapshot(m).Error)
	}
	if m.modeChange == nil {
		t.Fatal("apply queued no restart")
	}
	custom := m.modeChange.Preferences.ProfilePath
	if filepath.Dir(custom) != filepath.Join(root, "profiles", "custom") || custom == builtin {
		t.Fatal(custom)
	}
	saved, err := config.Load(custom)
	if err != nil || saved.Identity.UserName != "Alex" {
		t.Fatal(saved, err)
	}
	// The built-in file is never touched.
	if after, _ := os.ReadFile(builtin); string(after) != string(before) {
		t.Fatal("apply wrote to the built-in profile")
	}

	// The restart adopts the custom path, so the next apply reuses it.
	m.modeChange = nil
	m.application.Preferences.ProfilePath = custom
	m.editSettings("generation")
	replaceFormText(m, "0.8")
	m.applyForm()
	if m.chrome.DialogInfo().Kind != screen.NoDialog {
		t.Fatalf("apply did not finish: %q", formSnapshot(m).Error)
	}
	if m.modeChange == nil || m.modeChange.Preferences.ProfilePath != custom {
		t.Fatalf("custom profile not reused: %+v", m.modeChange)
	}
	saved, err = config.Load(custom)
	if err != nil || saved.Generation.Temperature != .8 {
		t.Fatal(saved, err)
	}

	m.modeChange = nil
	m.application.Preferences.ProfilePath = custom
	m.editSettings("prompts")
	focusSetting(m, "prompt-preset")
	replaceFormText(m, "coding")
	m.applyForm()
	saved, err = config.Load(custom)
	if m.chrome.DialogInfo().Kind != screen.NoDialog || err != nil || saved.Identity.PromptPreset != "coding" {
		t.Fatal("preset was not saved in custom setup", saved, err)
	}
	after, _ := os.ReadFile(builtin)
	if string(before) != string(after) {
		t.Fatal("bundled profile overwritten")
	}
	entries, _ := os.ReadDir(filepath.Dir(custom))
	if len(entries) != 1 {
		t.Fatal("autosave left extra copies", entries)
	}
}
