package config

import "fmt"

// Appearance changes rendering only, independently of session/model settings.
type Appearance struct {
	Frame      string `json:"frame,omitempty"`
	Density    string `json:"density,omitempty"`
	HideNames  bool   `json:"hide_names,omitempty"`
	HideLabels bool   `json:"hide_labels,omitempty"`
}

func (a Appearance) Validate() error {
	if a.Frame != "" && a.Frame != "minimal" && a.Frame != "outlined" {
		return fmt.Errorf("unknown frame style %q", a.Frame)
	}
	if a.Density != "" && a.Density != "compact" && a.Density != "comfortable" {
		return fmt.Errorf("unknown transcript density %q", a.Density)
	}
	return nil
}
