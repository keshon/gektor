package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// LoadPreferences reads strict application JSON. A missing file returns version-one defaults.
func LoadPreferences(path string) (Preferences, error) {
	preferences := Preferences{Version: 1}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return preferences, nil
	}

	if err != nil {
		return preferences, err
	}

	// A read-only close cannot change the decoded preferences.
	defer file.Close()

	err = decodeDocument(file, &preferences, "application preferences")
	if err != nil {
		return preferences, err
	}

	err = preferences.Validate()
	return preferences, err
}

// SavePreferences validates and replaces application JSON through a synced temporary file.
func SavePreferences(path string, preferences Preferences) error {
	err := preferences.Validate()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(preferences, "", "  ")
	if err != nil {
		return err
	}

	directory := filepath.Dir(path)
	err = os.MkdirAll(directory, 0o700)
	if err != nil {
		return err
	}

	file, err := os.CreateTemp(directory, ".application-*")
	if err != nil {
		return err
	}

	temporary := file.Name()
	// After rename the temporary path is absent; failed-save cleanup is best effort.
	defer os.Remove(temporary)

	data = append(data, '\n')
	_, err = file.Write(data)
	if err != nil {
		// Preserve the write or sync error if cleanup also fails.
		_ = file.Close()
		return err
	}

	err = file.Sync()
	if err != nil {
		// Preserve the write or sync error if cleanup also fails.
		_ = file.Close()
		return err
	}

	err = file.Close()
	if err != nil {
		return err
	}

	return os.Rename(temporary, path)
}
