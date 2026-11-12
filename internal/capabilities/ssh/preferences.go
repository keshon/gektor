package ssh

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/keshon/gektor/internal/credentials"
)

// Profiles owns the reference-only ssh.json file. Loading never connects or resolves credentials.
type Profiles struct {
	Path        string
	Credentials credentials.Store
}

func (s Profiles) Load() (map[string]Profile, error) {
	profiles, err := loadProfiles(s.Path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && profiles == nil) {
		return map[string]Profile{}, nil
	}
	return profiles, err
}

// Save validates the complete configuration before replacing it using a sibling temporary file.
func (s Profiles) Save(profiles map[string]Profile) error {
	if !filepath.IsAbs(s.Path) {
		return fmt.Errorf("SSH profiles require an absolute config path")
	}
	profiles = maps.Clone(profiles)
	err := validateProfiles(profiles)
	if err != nil {
		return err
	}

	raw, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if len(raw) > 64<<10 {
		return fmt.Errorf("SSH profiles exceed 64 KiB")
	}

	err = os.MkdirAll(filepath.Dir(s.Path), 0700)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.Path), ".ssh-*.json")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp) // Rename consumes the temporary file on success.

	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, s.Path)
}
