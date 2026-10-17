package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/filetxn"
	"github.com/keshon/tars/internal/speech"
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

var settingsSampleID atomic.Uint64

type settingsSampleMsg struct {
	id  uint64
	err error
}

func (m *model) previewSettingsSpeech() tea.Cmd {
	f := m.dialog.form
	ws := f.Workspace
	if ws.SampleCancel != nil {
		ws.SampleCancel()
		ws.SampleCancel = nil
		ws.SampleID++
		f.Error = "Sample stopped"
		return nil
	}
	staged, err := m.stagedSettings()
	if err != nil {
		f.Error = err.Error()
		return nil
	}
	current := m.audio().Profile.Speech
	if current == nil || staged.Speech == nil {
		f.Error = "This profile has no speech service"
		return nil
	}
	if speechSettingsChanged(staged.Speech, current) {
		f.Error = "Apply changed speech settings before playing its voice sample"
		return nil
	}
	if err := m.enableVoice(); err != nil {
		f.Error = err.Error()
		return nil
	}
	base := m.base
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	ws.SampleCancel = cancel
	ws.SampleID = settingsSampleID.Add(1)
	id := ws.SampleID
	service := m.runtime().Voice
	f.Error = "Playing current Chatterbox voice · Ctrl+P stops"
	return func() tea.Msg {
		out := speech.NewOutput(ctx, service, nil)
		err := out.Delta("Hello. This is my current voice. How does it sound to you?")
		if err != nil {
			out.Cancel()
		}
		finish := out.Finish()
		if err == nil {
			err = finish
		}
		return settingsSampleMsg{id, err}
	}
}
func (m *model) acceptSettingsSample(msg settingsSampleMsg) {
	if m.dialog == nil || m.dialog.form == nil || m.dialog.form.Workspace == nil {
		return
	}
	ws := m.dialog.form.Workspace
	if ws.SampleID != msg.id {
		return
	}
	if ws.SampleCancel != nil {
		ws.SampleCancel()
	}
	ws.SampleCancel = nil
	if msg.err != nil {
		m.dialog.form.Error = fmt.Sprintf("Voice sample: %v", msg.err)
	} else {
		m.dialog.form.Error = "Voice sample finished"
	}
}

func (m *model) hasSettingsSpeech() bool {
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.Workspace != nil && m.dialog.form.Workspace.Draft != nil {
		return m.dialog.form.Workspace.Draft.Base().Speech != nil
	}
	return m.audio() != nil && m.audio().Profile.Speech != nil
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
