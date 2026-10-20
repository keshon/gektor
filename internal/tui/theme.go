package tui

import (
	"fmt"

	"github.com/keshon/gektor/internal/config"
)

// applyTheme swaps the palette and everything derived from it.
//
// The transcript render cache is keyed on block, width, expansion and label
// mode — but not on styles — so a theme change has to drop it explicitly.
// The /theme command used to call setTheme alone, which left every settled
// block painted in the previous palette until something else forced a
// re-render.
func (m *model) applyTheme(name string) {
	setTheme(name)
	m.styles = defaultStyles()
	m.input.SetStyles(inputStyles(m.input.Styles()))
	m.note.SetStyles(textInputStyles(m.note.Styles()))
	if m.sessions != nil {
		m.sessions.filter.SetStyles(textInputStyles(m.sessions.filter.Styles()))
	}
	m.transcript.render.cache = nil
	m.refreshContent()
}

// persistTheme records the choice in the application preferences, so the
// next launch opens in it. Both entry points go through here.
func (m *model) persistTheme(name string) error {
	if m.application == nil {
		return nil
	}
	p, err := config.LoadPreferences(m.application.PreferencePath)
	if err != nil {
		return err
	}
	p.Theme = name
	if err := config.SavePreferences(m.application.PreferencePath, p); err != nil {
		return err
	}
	m.application.Preferences.Theme = name
	return nil
}

func (m *model) openTheme() {
	m.openForm("Theme", "Applies immediately; session settings stay unchanged.", "Apply", []formField{{Key: "theme", Label: "Theme", Value: currentPalette().name, Multiline: false}}, func(v map[string]string) error {
		name := v["theme"]
		if name != "dark" && name != "light" {
			return fmt.Errorf("choose Dark or Light")
		}
		if err := m.persistTheme(name); err != nil {
			return err
		}
		m.applyTheme(name)
		return nil
	})
	m.dialog.form.Choices = map[string][]string{"theme": {"dark", "light"}}
}
