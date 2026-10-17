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
	if err := profile.Validate(); err != nil {
		return err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	path = absolute
	relative := func(value string) string {
		if value == "" {
			return value
		}

		a, err := filepath.Abs(value)
		if err != nil {
			return value
		}
		v, err := filepath.Rel(filepath.Dir(path), a)
		if err != nil {
			return value
		}
		return v
	}
	profile.Provider.Runtime = relative(profile.Provider.Runtime)
	profile.Provider.ModelFile = relative(profile.Provider.ModelFile)
	profile.Provider.ModelRoot = relative(profile.Provider.ModelRoot)
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("choose a new profile filename: %w", err)
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	if _, err = f.Write(append(data, '\n')); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	ok = true
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
	temporary := filepath.Join(root, ".custom-"+strings.ToLower(rand.Text())+".json")
	if err := SaveCopy(profile, temporary); err != nil {
		return "", err
	}
	defer os.Remove(temporary)
	if err := os.Rename(temporary, target); err != nil {
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
	target := filepath.Join(root, "custom-"+strings.ToLower(rand.Text())+".json")
	if source != "" {
		absolute, err := filepath.Abs(source)
		if err != nil {
			return "", err
		}
		if strings.EqualFold(filepath.Dir(absolute), root) && strings.HasPrefix(filepath.Base(absolute), "custom-") && strings.EqualFold(filepath.Ext(absolute), ".json") {
			target = absolute
		}
	}
	return target, nil
}
