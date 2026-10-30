package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Preferences struct {
	Mode        string     `json:"mode,omitempty"`
	Theme       string     `json:"theme,omitempty"`
	Appearance  Appearance `json:"appearance,omitempty"`
	Version     int        `json:"version"`
	Workspace   string     `json:"workspace,omitempty"`
	ProfilePath string     `json:"profile_path,omitempty"`
	Profile     *Profile   `json:"profile,omitempty"`
	Harness     Harness    `json:"harness,omitempty"`
}

// UnlimitedSteps is the persisted ceiling used for an unlimited direct run.
const UnlimitedSteps = 999999

// Harness stores the operator's per-run step limit.
type Harness struct {
	MaxSteps int `json:"max_steps,omitempty"`
}

// Retired advice settings are accepted when reading existing preferences;
// the next save drops them. Other unknown keys still report an error.
func (h *Harness) UnmarshalJSON(data []byte) error {
	var saved struct {
		MaxSteps    int             `json:"max_steps"`
		Nudges      json.RawMessage `json:"nudges"`
		ThinkBudget json.RawMessage `json:"think_budget"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&saved); err != nil {
		return err
	}
	h.MaxSteps = saved.MaxSteps
	return nil
}

func (p Preferences) Validate() error {
	if err := p.Appearance.Validate(); err != nil {
		return err
	}
	if p.Mode != "" && p.Mode != "plan" && p.Mode != "act" {
		return fmt.Errorf("unknown workflow mode %q", p.Mode)
	}
	if p.Theme != "" && p.Theme != "dark" && p.Theme != "light" {
		return fmt.Errorf("unknown theme %q", p.Theme)
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
