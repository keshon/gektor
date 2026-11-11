package config

import "fmt"

// Validate checks application choices without changing them. The optional
// embedded profile is validated separately when selected for a run.
func (p Preferences) Validate() error {
	_, _, err := p.Harness.DelegateBudget()
	if err != nil {
		return err
	}

	err = p.Appearance.Validate()
	if err != nil {
		return err
	}

	if p.Mode != "" && p.Mode != "plan" && p.Mode != "act" {
		return fmt.Errorf("unknown workflow mode %q", p.Mode)
	}

	if p.Theme != "" && !ValidTheme(p.Theme) {
		return fmt.Errorf("unknown theme %q", p.Theme)
	}

	if p.Version != 1 {
		return fmt.Errorf("invalid application preferences")
	}

	return nil
}
