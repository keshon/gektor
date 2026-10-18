package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/keshon/tars/internal/harness"
)

type Preferences struct {
	Theme       string   `json:"theme,omitempty"`
	Version     int      `json:"version"`
	Workspace   string   `json:"workspace,omitempty"`
	ProfilePath string   `json:"profile_path,omitempty"`
	Profile     *Profile `json:"profile,omitempty"`
	Harness     Harness  `json:"harness,omitempty"`
}

// Harness is how the loop itself behaves: how much advice the model hears,
// and how much room a run gets.
//
// These live beside the profile, not inside it, because they describe the
// harness and not the backend. A profile says which model answers; a harness
// says how it is treated while it does. They also used to exist only as
// flags and live model fields, so a step budget or a nudge level set in the
// TUI was gone by the next launch and had to be typed again every time -
// which is the behaviour a settings screen is supposed to remove.
//
// Zero means the launch default, matching the flag defaults: an absent field
// and an explicit zero mean the same thing, so an older preferences file
// still reads.
type Harness struct {
	// MaxSteps is the per-run step budget.
	MaxSteps int `json:"max_steps,omitempty"`
	// Nudges is how much harness advice the model hears: full,
	// recovery, or off. Empty means full.
	Nudges string `json:"nudges,omitempty"`
	// ThinkBudget is the per-response reasoning budget in characters.
	ThinkBudget int `json:"think_budget,omitempty"`
}

func (p Preferences) Validate() error {
	if p.Theme != "" && p.Theme != "dark" && p.Theme != "light" {
		return fmt.Errorf("unknown theme %q", p.Theme)
	}
	switch p.Harness.Nudges {
	case "":
	default:
		if _, ok := harness.Parse(p.Harness.Nudges); !ok {
			return fmt.Errorf("unknown nudge level %q: want full, recovery or off", p.Harness.Nudges)
		}
	}
	if p.Version != 1 {
		return fmt.Errorf("invalid application preferences")
	}
	return nil
}
func LoadPreferences(path string) (Preferences, error) {
	p := Preferences{Version: 1}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	defer f.Close()
	// Reject unknown keys, as Profile.Load does. Plain Unmarshal silently
	// drops them, so a renamed field would quietly reset every user who had
	// set it - which is the same "it reset anyway" symptom the preferences
	// file is supposed to prevent.
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, fmt.Errorf("application preferences: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return p, fmt.Errorf("application preferences: expected one JSON document")
	}
	return p, p.Validate()
}
func SavePreferences(path string, p Preferences) error {
	if err := p.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".application-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
