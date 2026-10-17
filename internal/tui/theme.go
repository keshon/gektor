package tui

import (
	"fmt"
	"github.com/keshon/tars/internal/config"
)

func (m *model) openTheme() {
	m.openForm("Theme", "Applies immediately; session settings stay unchanged.", "Apply", []formField{{Key: "theme", Label: "Theme", Value: currentPalette().name, Multiline: false}}, func(v map[string]string) error {
		name := v["theme"]
		if name != "dark" && name != "light" {
			return fmt.Errorf("choose Dark or Light")
		}
		if m.application != nil {
			p, err := config.LoadPreferences(m.application.PreferencePath)
			if err != nil {
				return err
			}
			p.Theme = name
			if err := config.SavePreferences(m.application.PreferencePath, p); err != nil {
				return err
			}
			m.application.Preferences.Theme = name
		}
		setTheme(name)
		m.styles = defaultStyles()
		m.input.SetStyles(inputStyles(m.input.Styles()))
		m.note.SetStyles(textInputStyles(m.note.Styles()))
		m.transcript.render.cache = nil
		m.refreshContent()
		return nil
	})
	m.dialog.form.Choices = map[string][]string{"theme": {"dark", "light"}}
}
