package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Preferences struct {
	Theme       string   `json:"theme,omitempty"`
	Version     int      `json:"version"`
	Workspace   string   `json:"workspace,omitempty"`
	ProfilePath string   `json:"profile_path,omitempty"`
	Profile     *Profile `json:"profile,omitempty"`
}

func (p Preferences) Validate() error {
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
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if len(data) > 1<<20 {
		return p, fmt.Errorf("application preferences exceed 1 MiB")
	}
	if err = json.Unmarshal(data, &p); err != nil {
		return p, err
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
