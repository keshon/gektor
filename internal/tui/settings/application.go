package settings

import (
	"encoding/json"
	"strings"

	"github.com/keshon/gektor/internal/config"
)

// Application owns installation preferences and their persistence.
type Application struct {
	saved          string
	Preferences    config.Preferences
	PreferencePath string
	Temporary      bool
}

// Save persists the current launch's profile and Agent presentation choices.
// Temporary launches do not overwrite installation settings.
func (a *Application) Save(profile *config.Profile, mode string, appearance config.Appearance) error {
	if a == nil || a.Temporary {
		return nil
	}
	p := a.Preferences
	p.Mode, p.Appearance = strings.ToLower(mode), appearance
	if profile != nil {
		clone := profile.Clone()
		p.Profile = &clone
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if a.saved == string(data) {
		return nil
	}
	if err := config.SavePreferences(a.PreferencePath, p); err != nil {
		return err
	}
	a.saved = string(data)
	a.Preferences = p
	return nil
}

// SaveAppearance changes only presentation, including during a temporary launch.
func (a *Application) SaveAppearance(appearance config.Appearance) error {
	if a == nil {
		return nil
	}
	p, err := config.LoadPreferences(a.PreferencePath)
	if err != nil {
		return err
	}
	p.Appearance = appearance
	if err := config.SavePreferences(a.PreferencePath, p); err != nil {
		return err
	}
	a.Preferences.Appearance = appearance
	return nil
}

// SaveTheme changes only the installed theme.
func (a *Application) SaveTheme(theme string) error {
	if a == nil {
		return nil
	}
	p, err := config.LoadPreferences(a.PreferencePath)
	if err != nil {
		return err
	}
	p.Theme = theme
	if err := config.SavePreferences(a.PreferencePath, p); err != nil {
		return err
	}
	a.Preferences.Theme = theme
	return nil
}

// SaveHarness preserves installation settings when this launch is temporary.
func (a *Application) SaveHarness(harness config.Harness, profile *config.Profile, mode string, appearance config.Appearance) error {
	if a == nil {
		return nil
	}
	previous := a.Preferences.Harness
	a.Preferences.Harness = harness
	var err error
	if a.Temporary {
		var p config.Preferences
		p, err = config.LoadPreferences(a.PreferencePath)
		if err == nil {
			p.Harness = harness
			err = config.SavePreferences(a.PreferencePath, p)
		}
	} else {
		err = a.Save(profile, mode, appearance)
	}
	if err != nil {
		a.Preferences.Harness = previous
	}
	return err
}

// SaveMode preserves a temporary backend or workspace override.
func (a *Application) SaveMode(mode string, profile *config.Profile, appearance config.Appearance) error {
	if a == nil {
		return nil
	}
	if !a.Temporary {
		return a.Save(profile, mode, appearance)
	}
	p, err := config.LoadPreferences(a.PreferencePath)
	if err != nil {
		return err
	}
	p.Mode = strings.ToLower(mode)
	if err := config.SavePreferences(a.PreferencePath, p); err != nil {
		return err
	}
	a.Preferences.Mode = p.Mode
	return nil
}

// SaveThemeAppearance commits the two appearance choices together.
func (a *Application) SaveThemeAppearance(theme string, appearance config.Appearance) error {
	if a == nil {
		return nil
	}
	p, err := config.LoadPreferences(a.PreferencePath)
	if err != nil {
		return err
	}
	p.Theme, p.Appearance = theme, appearance
	if err := config.SavePreferences(a.PreferencePath, p); err != nil {
		return err
	}
	a.Preferences.Theme, a.Preferences.Appearance = theme, appearance
	return nil
}
