package tui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
)

func TestAppearanceStagesAndSavesThemeAndFrameTogether(t *testing.T) {
	defer setTheme("dark")
	m := settingsModel(t)
	before, err := config.LoadPreferences(m.application.PreferencePath)
	if err != nil {
		t.Fatal(err)
	}
	initial := m.agent.Appearance()
	m.openAppearance()
	focusSetting(m, "theme")
	m.chrome.Dialog.Form.Editor.SetValue("light")
	focusSetting(m, "frame")
	m.chrome.Dialog.Form.Editor.SetValue("minimal")
	m.formKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	after, err := config.LoadPreferences(m.application.PreferencePath)
	if err != nil || !reflect.DeepEqual(before, after) || m.agent.Appearance() != initial {
		t.Fatal("Cancel changed appearance")
	}
	m.openAppearance()
	focusSetting(m, "theme")
	m.chrome.Dialog.Form.Editor.SetValue("light")
	focusSetting(m, "frame")
	m.chrome.Dialog.Form.Editor.SetValue("minimal")
	m.applyForm()
	if m.chrome.Dialog != nil {
		t.Fatal("Appearance did not apply", m.chrome.Dialog.Form.Error)
	}
	after, err = config.LoadPreferences(m.application.PreferencePath)
	if err != nil || after.Theme != "light" || after.Appearance.Frame != "minimal" || !(m.agent.Appearance().Frame != "outlined") {
		t.Fatal("appearance not persisted", after, err)
	}
	if after.Workspace != before.Workspace || !reflect.DeepEqual(after.Profile, before.Profile) {
		t.Fatal("Appearance changed setup")
	}
}
