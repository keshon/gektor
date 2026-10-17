package tui

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/keshon/tars/internal/config"
)

func TestThemeChangesOnlyAppearanceAndPersists(t *testing.T) {
	defer setTheme("dark")
	m := settingsModel(t)
	profile := m.config.Profile
	path := filepath.Join(t.TempDir(), "application.json")
	original := config.Preferences{Version: 1, Profile: &profile}
	if err := config.SavePreferences(path, original); err != nil {
		t.Fatal(err)
	}
	m.application = &Application{Preferences: original, PreferencePath: path}
	before, err := os.ReadFile(m.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	draft := m.input.Value()
	m.openTheme()
	m.dialog.form.Editor.SetValue("light")
	m.applyForm()
	p, err := config.LoadPreferences(path)
	after, err2 := os.ReadFile(m.stateFile)
	if err != nil || err2 != nil || p.Theme != "light" || currentPalette().name != "light" || m.modeChange != nil || m.input.Value() != draft || string(before) != string(after) {
		t.Fatal("theme changed the session or did not persist")
	}
	if themeColor(colorAccent) != themeColor(colorUser) || themeColor(colorDim) != themeColor(colorSecondaryText) {
		t.Fatal("semantic roles disagree")
	}
}

func TestThemeSetsRendererDefaultsForUnstyledCells(t *testing.T) {
	defer setTheme("dark")
	for _, name := range []string{"dark", "light"} {
		setTheme(name)
		m := settingsModel(t)
		view := m.View()
		if view.BackgroundColor == nil || view.ForegroundColor == nil {
			t.Fatal("renderer inherits terminal colors")
		}
		r, g, b, a := view.BackgroundColor.RGBA()
		er, eg, eb, ea := lipgloss.Color(currentPalette().background).RGBA()
		if r != er || g != eg || b != eb || a != ea {
			t.Fatal("wrong frame background")
		}
		r, g, b, a = view.ForegroundColor.RGBA()
		er, eg, eb, ea = lipgloss.Color(themeColor(colorPopupText)).RGBA()
		if r != er || g != eg || b != eb || a != ea {
			t.Fatal("unstyled text uses terminal foreground")
		}
	}
}
