package tui

import (
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/screen"
)

// persistTheme records the choice in the application preferences, so the
// next launch opens in it. Both entry points go through here.
func (m *Model) persistTheme(name string) error { return m.application.SaveTheme(name) }

func (m *Model) openTheme() {
	m.chrome.OpenForm("Theme", "Apply saves this theme for future launches.", "Apply", []form.Field{{Key: "theme", Label: "Theme", Value: screen.CurrentPalette().Name, Multiline: false}}, func(v map[string]string) error {
		name := v["theme"]
		if !config.ValidTheme(name) {
			return fmt.Errorf("choose a theme from the list")
		}
		if err := m.persistTheme(name); err != nil {
			return err
		}
		m.applyTheme(name)
		return nil
	})
	m.chrome.Dialog.Form.Choices = map[string][]string{"theme": config.ThemeNames()}
}

// openAppearance stages presentation choices and saves them together on Apply.
func (m *Model) openAppearance() {
	m.chrome.OpenForm("Appearance", "Apply saves these choices for future launches.", "Apply", []form.Field{
		{Key: "theme", Label: "Theme", Value: screen.CurrentPalette().Name},
		{Key: "frame", Label: "Panel frame", Value: m.agent.Appearance().Frame},
	}, func(values map[string]string) error {
		theme, frame := values["theme"], values["frame"]
		if !config.ValidTheme(theme) {
			return fmt.Errorf("choose a theme from the list")
		}
		if frame != "outlined" && frame != "minimal" {
			return fmt.Errorf("choose an outlined or minimal frame")
		}
		appearance := m.agent.Appearance()
		appearance.Frame = frame
		if err := m.application.SaveThemeAppearance(theme, appearance); err != nil {
			return err
		}
		m.outlined = frame == "outlined"
		m.agent.ApplyAppearance(appearance)
		m.applyTheme(theme)
		return nil
	})
	m.chrome.Dialog.Form.Choices = map[string][]string{"theme": config.ThemeNames(), "frame": {"outlined", "minimal"}}
}

func (m *Model) applyTheme(name string) {
	screen.SetTheme(name)
	m.agent.RefreshTheme()
	m.syncPresentation()
}

func (m *Model) applicationAction(command string) bool {
	switch {
	case command == "about" || command == "/about":
		m.chrome.CloseHeader()
		m.openAbout()
		return true
	case strings.HasPrefix(command, "edit:"):
		if !m.workflowActive() {
			m.chrome.CloseHeader()
			m.editSettings(strings.TrimPrefix(command, "edit:"))
		}
		return true
	case command == "workspace":
		m.chrome.CloseHeader()
		m.openWorkspace()
		return true
	case command == "appearance":
		m.chrome.CloseHeader()
		m.openAppearance()
		return true
	case strings.HasPrefix(command, "/theme "):
		name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(command, "/theme ")))
		if name == "" {
			m.chrome.Open("Theme", config.ThemeNames())
			return true
		}
		if !config.ValidTheme(name) {
			m.chrome.Open("Theme", []string{"Choose a theme from the list."})
			return true
		}
		if err := m.persistTheme(name); err != nil {
			m.chrome.Open("Theme", []string{err.Error()})
			return true
		}
		m.applyTheme(name)
		return true
	}
	return false
}
