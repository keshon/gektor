package config

import "fmt"

// Appearance changes rendering only, independently of session/model settings.
type Appearance struct {
	Frame          string `json:"frame,omitempty"`
	HideNames      bool   `json:"hide_names,omitempty"`
	HideLabels     bool   `json:"hide_labels,omitempty"`
	HideSystem     bool   `json:"hide_system,omitempty"`
	HideTools      bool   `json:"hide_tools,omitempty"`
	ExpandThinking bool   `json:"expand_thinking,omitempty"`
}

// Validate accepts the default frame or a supported explicit frame.
func (a Appearance) Validate() error {
	if a.Frame != "" && a.Frame != "minimal" && a.Frame != "outlined" {
		return fmt.Errorf("unknown frame style %q", a.Frame)
	}

	return nil
}

// ThemeNames lists the persisted theme identifiers in picker order.
func ThemeNames() []string {
	return []string{"dark", "light", "one-half-dark", "one-half-light", "solarized-dark", "solarized-light"}
}

// ValidTheme reports whether name is a persisted theme identifier.
func ValidTheme(name string) bool {
	for _, theme := range ThemeNames() {
		if name == theme {
			return true
		}
	}

	return false
}
