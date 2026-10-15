package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/speech"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func focusSetting(m *model, key string) {
	for i, f := range m.dialog.form.Fields {
		if f.Key == key {
			m.focusForm(i)
			return
		}
	}
}
func TestCharacterPickerRejectsPortraitAndStagesCard(t *testing.T) {
	m := settingsModel(t)
	root := t.TempDir()
	bad := filepath.Join(root, "portrait.png")
	os.WriteFile(bad, []byte("not a card"), 0600)
	good := filepath.Join(root, "ada.json")
	os.WriteFile(good, []byte(`{"name":"Ada","description":"A helpful friend"}`), 0600)
	m.editSettings("character")
	focusSetting(m, "character")
	m.openSettingsPicker()
	p := m.dialog.form.picker
	m.acceptSettingsScan(settingsScanMsg{p.id, []string{bad, good}, nil})
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.err == "" || m.dialog.form.picker == nil {
		t.Fatal("invalid card selected")
	}
	p.Cursor = 1
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog.form.Editor.Value() != good || m.chat.config.Profile.Conversation.Character != "" {
		t.Fatal("card not staged separately")
	}
	m.closeDialog()
	if m.chat.config.Profile.Conversation.Character != "" {
		t.Fatal("cancel applied card")
	}
}
func TestLoadedProfileAppliesWithoutResettingHistory(t *testing.T) {
	m := settingsModel(t)
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "chat", Voice: "off", ProfilePath: "old.json"}}
	root := t.TempDir()
	p := cloneSettingsProfile(m.chat.config.Profile)
	p.Conversation.UserName = "Alex"
	p.Generation.Temperature = .8
	path := filepath.Join(root, "custom.json")
	if err := config.SaveCopy(p, path); err != nil {
		t.Fatal(err)
	}
	history := m.chat.current.History()
	id := m.stateFile
	m.editSettings("profiles")
	focusSetting(m, "load-profile")
	m.openSettingsPicker()
	picker := m.dialog.form.picker
	m.acceptSettingsScan(settingsScanMsg{picker.id, []string{path}, nil})
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.chat.config.Profile.Conversation.UserName != "User" {
		t.Fatal("load applied without Apply")
	}
	m.editSettings("generation")
	if m.dialog.form.Editor.Value() != "0.8" {
		t.Fatal("loaded settings not shown")
	}
	m.applyForm()
	if m.dialog != nil || m.stateFile != id || m.chat.config.Profile.Conversation.UserName != "Alex" || !reflect.DeepEqual(history, m.chat.current.History()) {
		t.Fatal("profile load lost session")
	}
	if m.profileSource() != "custom.json" {
		t.Fatal("loaded setup source was not updated", m.profileSource())
	}
}
func TestSampleCompletionCannotAffectClosedWorkspace(t *testing.T) {
	m := settingsModel(t)
	cfg := speech.Config{Recorder: "ffmpeg", PlayerBinary: "ffplay", Microphone: "Storm", MaxSeconds: 120, Speaking: speech.Speaking{Provider: "chatterbox", Variant: "nano", Binary: "python", Model: "models/nano"}, Listening: speech.Listening{Ownership: "external", URL: "http://127.0.0.1:19082"}}
	m.chat.config.Profile.Speech = &cfg
	m.chat.config.NewVoice = func(context.Context, string) (speech.Service, error) { return &voiceService{}, nil }
	m.editSettings("speech")
	cmd := m.previewSettingsSpeech()
	if cmd == nil {
		t.Fatal(m.dialog.form.error)
	}
	ws := m.dialog.form.workspace
	m.closeDialog()
	if ws.sampleCancel != nil {
		t.Fatal("close did not cancel sample")
	}
	m.acceptSettingsSample(cmd().(settingsSampleMsg))
	if m.dialog != nil {
		t.Fatal("late sample reopened workspace")
	}
}

func TestSettingsLibrariesPreview(t *testing.T) {
	dir := os.Getenv("TARS_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview opt-in")
	}
	os.MkdirAll(dir, 0700)
	m := settingsModel(t)
	m.width(tea.WindowSizeMsg{Width: 120, Height: 34})
	m.chat.config.Profile.Speech = &speech.Config{Recorder: "ffmpeg", PlayerBinary: "ffplay", Microphone: "Microphone (Storm Pro)", MaxSeconds: 120, Speaking: speech.Speaking{Provider: "chatterbox", Variant: "nano", Binary: "python", Model: "assets/models/speaking/chatterbox/nano"}, Listening: speech.Listening{Ownership: "external", URL: "http://127.0.0.1:19082"}}
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.editSettings("speech")
	write("speech-library")
	focusSetting(m, "microphone")
	m.openSettingsPicker()
	p := m.dialog.form.picker
	m.acceptSettingsScan(settingsScanMsg{p.id, []string{"Microphone (Storm Pro)", "Microphone (ARDOR GAMING Rune)"}, nil})
	write("microphone-picker")
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.editSettings("profiles")
	write("profiles-library")
	m.editSettings("character")
	focusSetting(m, "character")
	m.openSettingsPicker()
	p = m.dialog.form.picker
	m.acceptSettingsScan(settingsScanMsg{p.id, []string{"D:/Cards/Veyra.json", "E:/SillyTavern/characters/Ada.png"}, nil})
	write("character-picker")
}

func TestApplyAutomaticallySavesReusableCustomProfile(t *testing.T) {
	m := settingsModel(t)
	root := t.TempDir()
	original := cloneSettingsProfile(m.chat.config.Profile)
	builtin := filepath.Join(root, "profiles", "builtin.json")
	if err := config.SaveCopy(original, builtin); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(builtin)
	m.application = &Application{PreferencePath: filepath.Join(root, "var", "application.json"), Preferences: config.Preferences{Version: 1, Mode: "chat", Voice: "off", ProfilePath: builtin, Profile: &original}}
	m.editSettings("you")
	m.dialog.form.Editor.SetValue("Alex")
	m.applyForm()
	if m.dialog != nil {
		t.Fatal("apply did not finish", m.dialog.form.error)
	}
	custom := m.application.Preferences.ProfilePath
	if filepath.Dir(custom) != filepath.Join(root, "profiles", "custom") || custom == builtin {
		t.Fatal(custom)
	}
	saved, err := config.Load(custom)
	if err != nil || saved.Conversation.UserName != "Alex" {
		t.Fatal(saved, err)
	}
	m.editSettings("generation")
	m.dialog.form.Editor.SetValue("0.8")
	m.applyForm()
	if m.dialog != nil || m.application.Preferences.ProfilePath != custom {
		t.Fatal("custom profile not reused")
	}
	saved, err = config.Load(custom)
	if err != nil || saved.Generation.Temperature != .8 {
		t.Fatal(saved, err)
	}
	preferences, err := config.LoadPreferences(m.application.PreferencePath)
	if err != nil || preferences.ProfilePath != custom || preferences.Profile == nil || preferences.Profile.Generation.Temperature != .8 {
		t.Fatal("custom selection was not persisted", preferences, err)
	}
	m.editSettings("prompts")
	focusSetting(m, "prompt-preset")
	m.dialog.form.Editor.SetValue("coding")
	m.applyForm()
	saved, err = config.Load(custom)
	if m.dialog != nil || err != nil || saved.Conversation.PromptPreset != "coding" {
		t.Fatal("preset was not saved in custom setup", saved, err)
	}
	after, _ := os.ReadFile(builtin)
	if string(before) != string(after) {
		t.Fatal("bundled profile overwritten")
	}
	entries, _ := os.ReadDir(filepath.Dir(custom))
	if len(entries) != 1 {
		t.Fatal("autosave left extra copies", entries)
	}
}
