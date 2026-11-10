package agent

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
func (m *Model) applyTheme(name string) {
	setTheme(name)
	m.styles = defaultStyles()
	m.input.SetStyles(inputStyles(m.input.Styles()))
	m.note.SetStyles(textInputStyles(m.note.Styles()))
	if m.sessions != nil {
		m.sessions.filter.SetStyles(textInputStyles(m.sessions.filter.Styles()))
	}
	m.transcript.render.cache = nil
	m.refreshContent()
	m.inspector.pane.render.cache = nil
	m.refreshInspector()
}

// persistTheme records the choice in the application preferences, so the
// next launch opens in it. Both entry points go through here.
func (m *Model) persistTheme(name string) error {
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

func (m *Model) openTheme() {
	m.openForm("Theme", "Apply saves this theme for future launches.", "Apply", []formField{{Key: "theme", Label: "Theme", Value: currentPalette().Name, Multiline: false}}, func(v map[string]string) error {
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
