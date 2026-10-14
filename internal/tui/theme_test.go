package tui

import (
	"github.com/keshon/tars/internal/config"
	"path/filepath"
	"testing"
)

func TestThemeChangesOnlyAppearanceAndPersists(t *testing.T) {
	defer setTheme("dark")
	m := settingsModel(t)
	profile := m.chat.config.Profile
	path := filepath.Join(t.TempDir(), "application.json")
	original := config.Preferences{Version: 1, Mode: "chat", Voice: "off", Profile: &profile}
	if err := config.SavePreferences(path, original); err != nil {
		t.Fatal(err)
	}
	m.application = &Application{Preferences: original, PreferencePath: path}
	history := m.chat.current.History()
	draft := m.input.Value()
	m.openTheme()
	m.dialog.form.editor.SetValue("light")
	m.applyForm()
	p, err := config.LoadPreferences(path)
	if err != nil || p.Theme != "light" || currentPalette().name != "light" || m.modeChange != nil || m.input.Value() != draft || len(m.chat.current.History()) != len(history) {
		t.Fatal("theme changed conversation or did not persist")
	}
	if themeColor(colorAccent) != themeColor(colorUser) || themeColor(colorDim) != themeColor(colorSecondaryText) {
		t.Fatal("semantic roles disagree")
	}
}
