package executables

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Save replaces validated machine preferences atomically and clears probe observations.
// Callers apply changes only at an idle application boundary.
func (r *Resolver) Save(config Config) error {
	if r.ConfigPath == "" {
		return fmt.Errorf("applications configuration path is unavailable")
	}
	for name, setting := range config.Applications {
		if _, ok := specs[name]; !ok {
			return fmt.Errorf("unknown application %q", name)
		}
		if setting.Path != "" && !filepath.IsAbs(setting.Path) {
			return fmt.Errorf("configured %s path must be absolute", name)
		}
		if strings.ContainsAny(setting.Path, "\r\n\x00") {
			return fmt.Errorf("invalid %s executable path", name)
		}
	}
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if len(raw) > 32768 {
		return fmt.Errorf("applications.json exceeds 32 KiB")
	}
	dir := filepath.Dir(r.ConfigPath)
	err = os.MkdirAll(dir, 0700)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".applications-*.json")
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
	err = os.Rename(temp, r.ConfigPath)
	if err != nil {
		return err
	}
	r.mu.Lock()
	clear(r.cache)
	r.mu.Unlock()
	return nil
}
