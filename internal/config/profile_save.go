package config

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SaveCopy writes a new portable profile; existing profiles are never overwritten.
func SaveCopy(profile Profile, path string) error {
	err := profile.Validate()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}

	directory := filepath.Dir(path)
	err = os.MkdirAll(directory, 0o700)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("choose a new profile filename: %w", err)
	}

	saved := false
	defer func() {
		// Cleanup is best effort; preserve the write, sync, or close error.
		_ = file.Close()
		if !saved {
			_ = os.Remove(path)
		}
	}()

	data = append(data, '\n')
	_, err = file.Write(data)
	if err != nil {
		return err
	}

	err = file.Sync()
	if err != nil {
		return err
	}

	err = file.Close()
	if err != nil {
		return err
	}

	saved = true
	return nil
}

// SaveCustom creates or updates an application-managed custom setup atomically.
// Bundled profiles and files outside the custom directory are never overwritten.
func SaveCustom(profile Profile, directory, source string) (string, error) {
	target, err := CustomProfilePath(directory, source)
	if err != nil {
		return "", err
	}

	root := filepath.Dir(target)
	suffix := rand.Text()
	name := ".custom-" + strings.ToLower(suffix) + ".json"
	temporary := filepath.Join(root, name)
	err = SaveCopy(profile, temporary)
	if err != nil {
		return "", err
	}

	// After rename the temporary path is absent; failed-save cleanup is best effort.
	defer os.Remove(temporary)
	err = os.Rename(temporary, target)
	if err != nil {
		return "", err
	}

	return target, nil
}

// CustomProfilePath chooses an application-managed target without writing it.
// An existing source is reused only inside the custom directory.
func CustomProfilePath(directory, source string) (string, error) {
	root, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}

	if source != "" {
		absolute, sourceErr := filepath.Abs(source)
		if sourceErr != nil {
			return "", sourceErr
		}

		sourceDirectory := filepath.Dir(absolute)
		name := filepath.Base(absolute)
		extension := filepath.Ext(absolute)
		managed := strings.EqualFold(sourceDirectory, root) && strings.HasPrefix(name, "custom-") && strings.EqualFold(extension, ".json")
		if managed {
			return absolute, nil
		}
	}

	suffix := rand.Text()
	name := "custom-" + strings.ToLower(suffix) + ".json"
	target := filepath.Join(root, name)
	return target, nil
}
