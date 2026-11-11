package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/gektor/internal/config"
)

func (m *Model) stagedSettings() (config.Profile, error) {
	m.chrome.Dialog.Form.StorePage()
	return m.chrome.Dialog.Form.EditSession.Draft.Resolve()
}

func (m *Model) saveSettingsCopy() {
	p, err := m.stagedSettings()
	if err != nil {
		m.chrome.Dialog.Form.Error = err.Error()
		return
	}
	name := "custom"
	for _, field := range m.chrome.Dialog.Form.Fields {
		if field.Key == "profile-name" {
			name = strings.TrimSpace(field.Value)
		}
	}
	if name == "" || strings.ContainsAny(name, "/\\:") || name == "." || name == ".." {
		m.chrome.Dialog.Form.Error = "Use a profile name without directory separators"
		return
	}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		name += ".json"
	}
	exe, _ := os.Executable()
	root := filepath.Join(filepath.Dir(exe), "profiles")
	path := filepath.Join(root, name)
	if err := config.SaveCopy(p, path); err != nil {
		m.chrome.Dialog.Form.Error = err.Error()
		return
	}
	m.chrome.Dialog.Form.Error = "Saved copy: " + path
}

func (m *Model) saveCustomSettings(profile config.Profile) error {
	dir, source, ok := m.customProfileTarget()
	if !ok {
		return nil
	}
	path, err := config.SaveCustom(profile, dir, source)
	if err != nil {
		return fmt.Errorf("save custom profile: %w", err)
	}
	ws := m.chrome.Dialog.Form.EditSession
	ws.LoadedPath = path
	return nil
}

// customProfileTarget reports where a changed form should be saved: the
// custom directory, and the currently loaded profile to reuse its name.
func (m *Model) customProfileTarget() (dir, source string, ok bool) {
	if m.application == nil || m.application.PreferencePath == "" || m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil || m.chrome.Dialog.Form.EditSession == nil {
		return "", "", false
	}
	ws := m.chrome.Dialog.Form.EditSession
	changed := ws.PersistCurrent
	for _, section := range ws.Draft.ChangedSections() {
		if section != "profiles" && section != "workspace" {
			changed = true
			break
		}
	}
	if !changed {
		return "", "", false
	}
	root := filepath.Dir(m.application.PreferencePath)
	if filepath.Base(root) == "var" {
		root = filepath.Dir(root)
	}
	source = m.application.Preferences.ProfilePath
	if ws.LoadedPath != "" {
		source = ws.LoadedPath
	}
	return filepath.Join(root, "profiles", "custom"), source, true
}
