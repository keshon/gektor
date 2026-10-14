package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"fmt"
	"github.com/keshon/tars/internal/character"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/speech"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

func pickerFiles(key string) bool {
	return key == "model-file" || key == "character" || key == "load-profile" || key == "app-profile" || key == "app-workspace"
}
func pickerControl(key string) bool { return pickerFiles(key) || key == "model" || key == "microphone" }
func pickerAccepts(key, path string) bool {
	switch key {
	case "model-file":
		return strings.EqualFold(filepath.Ext(path), ".gguf")
	case "character":
		ext := strings.ToLower(filepath.Ext(path))
		return ext == ".png" || ext == ".json"
	case "app-profile", "load-profile":
		return strings.EqualFold(filepath.Ext(path), ".json")
	}
	return false
}
func catalogFiles(root, key string) ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && pickerAccepts(key, path) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
func cloneSettingsProfile(p config.Profile) config.Profile {
	if p.Conversation != nil {
		v := *p.Conversation
		p.Conversation = &v
	}
	if p.Speech != nil {
		v := *p.Speech
		p.Speech = &v
	}
	return p
}
func (m *model) settingsBase() config.Profile {
	ws := m.dialog.form.workspace
	if ws.loaded != nil {
		return cloneSettingsProfile(*ws.loaded)
	}
	return cloneSettingsProfile(m.audio().Profile)
}
func (m *model) stagedSettings() (config.Profile, error) {
	m.storeSettingsPage()
	ws := m.dialog.form.workspace
	p := m.settingsBase()
	for category, fields := range ws.cache {
		if reflect.DeepEqual(fields, ws.original[category]) {
			continue
		}
		values := map[string]string{}
		for _, field := range fields {
			values[field.key] = field.value
		}
		if category == "application" {
			continue
		}
		if err := applySettingsValues(&p, category, values); err != nil {
			return p, err
		}
	}
	return p, p.Validate()
}
func (m *model) saveSettingsCopy() {
	p, err := m.stagedSettings()
	if err != nil {
		m.dialog.form.error = err.Error()
		return
	}
	name := "chat-custom"
	for _, field := range m.dialog.form.fields {
		if field.key == "profile-name" {
			name = strings.TrimSpace(field.value)
		}
	}
	if name == "" || strings.ContainsAny(name, "/\\:") || name == "." || name == ".." {
		m.dialog.form.error = "Use a profile name without directory separators"
		return
	}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		name += ".json"
	}
	exe, _ := os.Executable()
	root := filepath.Join(filepath.Dir(exe), "profiles")
	path := filepath.Join(root, name)
	if err := config.SaveCopy(p, path); err != nil {
		m.dialog.form.error = err.Error()
		return
	}
	m.dialog.form.error = "Saved copy: " + path
}
func catalogDetail(key, path string) string {
	if key == "character" {
		card, err := character.Load(path)
		if err != nil {
			return "Unavailable card: " + err.Error()
		}
		return card.Name + " · " + strings.ReplaceAll(card.Description, "\n", " ") + " · " + path
	}
	if key == "load-profile" || key == "app-profile" {
		p, err := config.Load(path)
		if err != nil {
			return "Unavailable profile: " + err.Error()
		}
		return p.Provider.Kind + " · " + p.Provider.Ownership + " · " + p.Provider.Model + " · " + path
	}
	return path
}

// Async discovery captures the configured recorder but never captures audio.
func (m *model) microphonePicker(p *settingsPicker) tea.Cmd {
	cfg := m.settingsBase()
	id := p.id
	p.loading = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		items, err := speech.Microphones(ctx, cfg.Speech.Recorder)
		return settingsScanMsg{id, items, err}
	}
}

type settingsSampleMsg struct {
	id  uint64
	err error
}

func (m *model) previewSettingsSpeech() tea.Cmd {
	f := m.dialog.form
	ws := f.workspace
	if ws.sampleCancel != nil {
		ws.sampleCancel()
		ws.sampleCancel = nil
		ws.sampleID++
		f.error = "Sample stopped"
		return nil
	}
	staged, err := m.stagedSettings()
	if err != nil {
		f.error = err.Error()
		return nil
	}
	current := m.audio().Profile.Speech
	if current == nil || staged.Speech == nil {
		f.error = "This profile has no speech service"
		return nil
	}
	a, b := *staged.Speech, *current
	a.Enabled = b.Enabled
	if a != b {
		f.error = "Apply changed speech settings before playing its voice sample"
		return nil
	}
	if err := m.enableVoice(); err != nil {
		f.error = err.Error()
		return nil
	}
	base := m.base
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	ws.sampleCancel = cancel
	ws.sampleID = settingsScanID.Add(1)
	id := ws.sampleID
	service := m.voice
	f.error = "Playing current Chatterbox voice · Ctrl+P stops"
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
	if m.dialog == nil || m.dialog.form == nil || m.dialog.form.workspace == nil {
		return
	}
	ws := m.dialog.form.workspace
	if ws.sampleID != msg.id {
		return
	}
	if ws.sampleCancel != nil {
		ws.sampleCancel()
	}
	ws.sampleCancel = nil
	if msg.err != nil {
		m.dialog.form.error = fmt.Sprintf("Voice sample: %v", msg.err)
	} else {
		m.dialog.form.error = "Voice sample finished"
	}
}

func (m *model) hasSettingsSpeech() bool {
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.workspace != nil && m.dialog.form.workspace.loaded != nil {
		return m.dialog.form.workspace.loaded.Speech != nil
	}
	return m.audio() != nil && m.audio().Profile.Speech != nil
}

type settingsDetailMsg struct {
	id         uint64
	path, text string
}

func (m *model) queueSettingsDetail() {
	if m.dialog == nil || m.dialog.form == nil || m.dialog.form.picker == nil {
		return
	}
	p := m.dialog.form.picker
	if !pickerFiles(p.field) || len(p.filtered) == 0 || p.loading {
		return
	}
	path := p.filtered[p.cursor]
	if path == p.detailPath {
		return
	}
	p.detailPath = path
	p.detail = ""
	id, key := p.id, p.field
	m.pendingCommands = append(m.pendingCommands, func() tea.Msg {
		text := catalogDetail(key, path)
		if key == "model-file" {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				text = fmt.Sprintf("%.2f GiB · %s", float64(info.Size())/(1<<30), text)
			}
		}
		return settingsDetailMsg{id, path, text}
	})
}
func (m *model) acceptSettingsDetail(msg settingsDetailMsg) {
	if m.dialog == nil || m.dialog.form == nil || m.dialog.form.picker == nil {
		return
	}
	p := m.dialog.form.picker
	if p.id == msg.id && p.detailPath == msg.path {
		p.detail = msg.text
	}
}
