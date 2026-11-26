package tui

import (
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/screen"
)

// persistTheme records the choice in the application preferences, so the
// next launch opens in it. Both entry points go through here.
func (m *Model) persistTheme(name string) error { return m.application.SaveTheme(name) }

func (m *Model) openTheme() {
	f := themeChoiceForm()
	f.Title = "Theme"
	f.Fields = []form.Field{{Key: "theme", Label: "Theme", Value: screen.CurrentPalette().Name}}
	m.openForm(f, func(v map[string]string) error {
		name := v["theme"]
		if !config.ValidTheme(name) {
			return fmt.Errorf("choose a theme from the list")
		}
		saveErr := m.persistTheme(name)
		if saveErr != nil {
			return saveErr
		}
		m.applyTheme(name)
		return nil
	})
}

// openAppearance stages presentation choices and saves them together on Apply.
func (m *Model) openAppearance() {
	f := themeChoiceForm()
	f.Title = "Appearance"
	appearance := m.agent.Appearance()
	caret := appearance.Caret
	if caret == "" {
		caret = "bar"
	}
	animation := appearance.StatusAnimation
	if animation == "" {
		animation = config.DefaultStatusAnimation(appearance.Style == "modern")
	}
	f.Fields = []form.Field{
		{Key: "theme", Label: "Theme", Value: screen.CurrentPalette().Name},
		{Key: "frame", Label: "Borders", Value: appearance.Frame},
		{Key: "style", Label: "UI style", Value: appearance.Style},
		{Key: "caret", Label: "Caret", Value: caret},
		{Key: "status_animation", Label: "Status animation", Value: animation},
	}

	f.Choices["status_animation"] = []string{"braille", "square", "text"}
	f.Labels["status_animation"] = map[string]string{"braille": "Braille spinner", "square": "Blinking square", "text": "Blinking text"}
	f.InlineChoices["status_animation"] = 3
	f.Choices["caret"] = []string{"block", "bar", "underline"}
	f.Labels["caret"] = map[string]string{"block": "Block", "bar": "Vertical bar", "underline": "Underline"}
	f.InlineChoices["caret"] = 3
	f.Choices["style"] = []string{"commander", "modern"}
	f.Labels["style"] = map[string]string{"commander": "Commander", "modern": "Modern"}
	f.InlineChoices["style"] = 2
	f.Choices["frame"] = []string{"outlined", "minimal"}
	f.Labels["frame"] = map[string]string{"outlined": "Visible", "minimal": "Hidden"}
	f.InlineChoices["frame"] = 2
	m.openForm(f, func(values map[string]string) error {
		theme, frame := values["theme"], values["frame"]
		if !config.ValidTheme(theme) {
			return fmt.Errorf("choose a theme from the list")
		}
		if frame != "outlined" && frame != "minimal" {
			return fmt.Errorf("choose visible or hidden borders")
		}

		appearance := m.agent.Appearance()
		appearance.Frame = frame
		appearance.Style = values["style"]
		appearance.Caret = values["caret"]
		appearance.StatusAnimation = values["status_animation"]
		if err := appearance.Validate(); err != nil {
			return err
		}
		saveErr := m.application.SaveThemeAppearance(theme, appearance)
		if saveErr != nil {
			return saveErr
		}

		m.applyAppearance(theme, appearance)
		return nil
	})
}

func setAppearance(theme string, appearance config.Appearance) {
	screen.SetAppearance(screen.Appearance{
		Name:            theme,
		Caret:           appearance.Caret,
		StatusAnimation: appearance.StatusAnimation,
		Modern:          appearance.Style == "modern",
		Outlined:        appearance.Frame == "outlined",
	})
}

func (m *Model) applyAppearance(theme string, appearance config.Appearance) {
	setAppearance(theme, appearance)
	m.agent.ApplyHistoryAppearance(appearance)
	m.syncPresentation()
}

func (m *Model) applyTheme(name string) {
	screen.SetTheme(name)
	m.agent.RefreshTheme()
	m.syncPresentation()
}

func (m *Model) applicationAction(command string) bool {
	switch {
	case command == "settings" || command == "/settings":
		m.chrome.Header = screen.MenuState{Global: true, Focused: true, Menu: "settings"}
		m.chrome.Header.Row = m.chrome.HeaderActionRow(0, 1)
		return true
	case command == "about" || command == "/about":
		m.chrome.CloseHeader()
		m.openAbout()
		return true
	case strings.HasPrefix(command, "edit:"):
		m.chrome.CloseHeader()
		m.editSettings(strings.TrimPrefix(command, "edit:"))
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
		saveErr := m.persistTheme(name)
		if saveErr != nil {
			m.chrome.Open("Theme", []string{saveErr.Error()})
			return true
		}
		m.applyTheme(name)
		return true
	}
	return false
}

func themeChoiceForm() screen.Form {
	labels := map[string]string{}
	for _, name := range config.ThemeNames() {
		labels[name] = settings.ChoiceLabel("theme", name)
	}
	return screen.Form{
		Hint: "Applies immediately to both modes and is remembered for future launches.", ApplyLabel: "Apply",
		Choices:        map[string][]string{"theme": config.ThemeNames()},
		Labels:         map[string]map[string]string{"theme": labels},
		InlineChoices:  map[string]int{"theme": len(labels)},
		NoSearchFields: map[string]bool{"theme": true},
	}
}
