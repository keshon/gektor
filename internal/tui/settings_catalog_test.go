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
	for i, f := range m.dialog.form.fields {
		if f.key == key {
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
	p.cursor = 1
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog.form.editor.Value() != good || m.chat.config.Profile.Conversation.Character != "" {
		t.Fatal("card not staged separately")
	}
	m.closeDialog()
	if m.chat.config.Profile.Conversation.Character != "" {
		t.Fatal("cancel applied card")
	}
}
func TestLoadedProfileAppliesWithoutResettingHistory(t *testing.T) {
	m := settingsModel(t)
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
	if m.dialog.form.editor.Value() != "0.8" {
		t.Fatal("loaded settings not shown")
	}
	m.applyForm()
	if m.dialog != nil || m.stateFile != id || m.chat.config.Profile.Conversation.UserName != "Alex" || !reflect.DeepEqual(history, m.chat.current.History()) {
		t.Fatal("profile load lost session")
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
