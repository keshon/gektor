package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/filetxn"
	"github.com/keshon/tars/internal/tui/settings"
)

func (m *model) stagedSettings() (config.Profile, error) {
	m.storeSettingsPage()
	return m.dialog.form.Workspace.Draft.Resolve()
}
func (m *model) saveSettingsCopy() {
	p, err := m.stagedSettings()
	if err != nil {
		m.dialog.form.Error = err.Error()
		return
	}
	name := "chat-custom"
	for _, field := range m.dialog.form.Fields {
		if field.Key == "profile-name" {
			name = strings.TrimSpace(field.Value)
		}
	}
	if name == "" || strings.ContainsAny(name, "/\\:") || name == "." || name == ".." {
		m.dialog.form.Error = "Use a profile name without directory separators"
		return
	}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		name += ".json"
	}
	exe, _ := os.Executable()
	root := filepath.Join(filepath.Dir(exe), "profiles")
	path := filepath.Join(root, name)
	if err := config.SaveCopy(p, path); err != nil {
		m.dialog.form.Error = err.Error()
		return
	}
	m.dialog.form.Error = "Saved copy: " + path
}

type settingsDetailMsg = settings.DetailMsg

func (m *model) queueSettingsDetail() {
	if m.dialog != nil && m.dialog.form != nil {
		if cmd := m.dialog.form.DetailCommand(); cmd != nil {
			m.pendingCommands = append(m.pendingCommands, cmd)
		}
	}
}
func (m *model) acceptSettingsDetail(msg settingsDetailMsg) {
	if m.dialog != nil && m.dialog.form != nil {
		m.dialog.form.AcceptDetail(msg)
	}
}
func (m *model) saveCustomSettings(profile config.Profile) error {
	path, err := m.customSettingsPath()
	if err != nil || path == "" {
		return err
	}
	if err := filetxn.Replace([]filetxn.Write{{Path: path, Save: func(staged string) error { return config.SaveCopy(profile, staged) }}}); err != nil {
		return fmt.Errorf("save custom profile: %w", err)
	}
	ws := m.dialog.form.Workspace
	ws.LoadedPath, ws.AutoSaved = path, true
	return nil
}

func (m *model) customSettingsPath() (string, error) {
	if m.application == nil || m.application.PreferencePath == "" || m.dialog == nil || m.dialog.form == nil || m.dialog.form.Workspace == nil {
		return "", nil
	}
	ws := m.dialog.form.Workspace
	changed := false
	for _, section := range ws.Draft.ChangedSections() {
		if section != "profiles" && section != "application" {
			changed = true
			break
		}
	}
	if !changed {
		return "", nil
	}
	root := filepath.Dir(m.application.PreferencePath)
	if filepath.Base(root) == "var" {
		root = filepath.Dir(root)
	}
	source := m.application.Preferences.ProfilePath
	if ws.LoadedPath != "" {
		source = ws.LoadedPath
	}
	return config.CustomProfilePath(filepath.Join(root, "profiles", "custom"), source)
}

var pickerFiles = settings.PickerFiles

var pickerControl = settings.PickerControl

var pickerAccepts = settings.PickerAccepts

var catalogFiles = settings.CatalogFiles

var catalogDetail = settings.CatalogDetail
