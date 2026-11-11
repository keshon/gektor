package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
)

func TestEveryThemeCanBeChosenSavedAndReopened(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		t.Run(theme, func(t *testing.T) {
			m := settingsModel(t)
			*m.application = Application{PreferencePath: filepath.Join(t.TempDir(), "application.json")}
			m.command("/theme " + theme)
			p, err := config.LoadPreferences(m.application.PreferencePath)
			if err != nil || p.Theme != theme || currentPalette().Name != theme {
				t.Fatalf("command did not save and apply %s: %+v, %v", theme, p, err)
			}
			m.openTheme()
			if m.chrome.Dialog.Form.Editor.Value() != theme {
				t.Fatal("theme form lost the saved selection")
			}
			if settings.ChoiceLabel("theme", theme) == theme {
				t.Fatal("picker exposes a storage identifier instead of a readable name")
			}
		})
	}
}

func TestThemeChangesOnlyAppearanceAndPersists(t *testing.T) {
	defer setTheme("dark")
	m := settingsModel(t)
	profile := m.profile.Profile
	path := filepath.Join(t.TempDir(), "application.json")
	original := config.Preferences{Version: 1, Profile: &profile}
	if err := config.SavePreferences(path, original); err != nil {
		t.Fatal(err)
	}
	*m.application = Application{Preferences: original, PreferencePath: path}
	before, err := os.ReadFile(stateFileOf(m))
	if err != nil {
		t.Fatal(err)
	}
	draft := draftOf(m)
	m.openTheme()
	m.chrome.Dialog.Form.Editor.SetValue("light")
	m.applyForm()
	p, err := config.LoadPreferences(path)
	after, err2 := os.ReadFile(stateFileOf(m))
	if err != nil || err2 != nil || p.Theme != "light" || currentPalette().Name != "light" || m.modeChange != nil || draftOf(m) != draft || string(before) != string(after) {
		t.Fatal("theme changed the session or did not persist")
	}
	if themeColor(colorAccent) != themeColor(screen.ColorUser) || themeColor(screen.ColorDim) != themeColor(screen.ColorSecondaryText) {
		t.Fatal("semantic roles disagree")
	}
}

func TestInlineThemeChangesStayInDraftUntilApply(t *testing.T) {
	defer setTheme("dark")
	setTheme("dark")
	m := settingsModel(t)
	path := filepath.Join(t.TempDir(), "application.json")
	p := config.Preferences{Version: 1, Theme: "dark"}
	if err := config.SavePreferences(path, p); err != nil {
		t.Fatal(err)
	}
	*m.application = Application{PreferencePath: path, Preferences: p}
	m.openTheme()
	m.formKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.chrome.Dialog.Form.Picker != nil || m.chrome.Dialog.Form.Editor.Value() != "light" || currentPalette().Name != "dark" {
		t.Fatal("inline choice opened another modal or applied before confirmation")
	}
	m.formKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	saved, err := config.LoadPreferences(path)
	if err != nil || saved.Theme != "dark" {
		t.Fatal("cancel saved the pending theme")
	}
	m.openTheme()
	m.formKey(tea.KeyPressMsg{Code: tea.KeyRight})
	m.formKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	saved, err = config.LoadPreferences(path)
	if err != nil || saved.Theme != "light" || currentPalette().Name != "light" || m.chrome.Dialog != nil {
		t.Fatal("Apply did not save and apply the staged choice")
	}
}
