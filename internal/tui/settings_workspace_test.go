package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSettingsAcrossCategoriesApplyToCurrentSession(t *testing.T) {
	m := settingsModel(t)
	path := m.stateFile
	history := m.chat.current.History()
	draft := m.input.Value()
	m.editSettings("generation")
	m.dialog.form.Editor.SetValue("0.8")
	m.editSettings("you")
	m.focusForm(0)
	m.dialog.form.Editor.SetValue("Alex")
	m.editSettings("generation")
	if m.dialog.form.Editor.Value() != "0.8" {
		t.Fatal("category switch lost staged edit")
	}
	m.applyForm()
	if m.dialog != nil || m.stateFile != path || m.input.Value() != draft || m.chat.config.Profile.Conversation.UserName != "Alex" || m.chat.config.Settings.Temperature != .8 {
		t.Fatal("apply reset session or lost staged values")
	}
	if !reflect.DeepEqual(history, m.chat.current.History()) {
		t.Fatal("history changed")
	}
	archive, err := chat.ReadArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	if archive.Config.Temperature != .8 || !reflect.DeepEqual(archive.History, history) {
		t.Fatal("archive did not preserve history and new settings")
	}
}
func TestSettingsCancelAllCategories(t *testing.T) {
	m := settingsModel(t)
	original := m.chat.current
	profile := m.chat.config.Profile
	m.editSettings("generation")
	m.dialog.form.Editor.SetValue("0.9")
	m.editSettings("you")
	m.focusForm(0)
	m.dialog.form.Editor.SetValue("Changed")
	m.closeDialog()
	if m.chat.current != original || m.chat.config.Profile.Conversation.UserName != profile.Conversation.UserName || m.chat.config.Settings.Temperature != 0 {
		t.Fatal("cancel applied staged values")
	}
}
func TestPickerSearchBackAndStaleScan(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("model")
	m.dialog.form.Fields[0].Value = "managed"
	m.dialog.form.Editor.SetValue("managed")
	for i, f := range m.dialog.form.Fields {
		if f.Key == "model-file" {
			m.focusForm(i)
			break
		}
	}
	m.openSettingsPicker()
	p := m.dialog.form.picker
	m.acceptSettingsScan(settingsScanMsg{p.id, []string{"C:/models/Qwen.gguf", "C:/models/Chronos.gguf"}, nil})
	m.pickerKey(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m.pickerKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if len(p.filtered) != 1 || !strings.Contains(p.filtered[0], "Chronos") {
		t.Fatal("search did not narrow models")
	}
	previous := m.dialog.form.Editor.Value()
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog.form.Editor.Value() != previous || m.dialog.form.picker != nil {
		t.Fatal("picker cancel changed model")
	}
	m.acceptSettingsScan(settingsScanMsg{p.id, []string{"late.gguf"}, nil})
	if m.dialog.form.picker != nil {
		t.Fatal("late scan reopened picker")
	}
}

func TestSettingsPasteRespectsNestedFocus(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("model")
	for i, field := range m.dialog.form.Fields {
		if field.Key == "kind" {
			m.focusForm(i)
			break
		}
	}
	f := m.dialog.form
	before := f.Editor.Value()
	m.openSettingsPicker()
	p := f.picker
	p.items = []string{"llama", "openai", "kobold"}
	p.filter()
	m.Update(tea.PasteMsg{Content: "open"})
	if p.search.Value() != "open" || !reflect.DeepEqual(p.filtered, []string{"openai"}) {
		t.Fatal("picker paste did not update search and filtered options")
	}
	if f.Editor.Value() != before || f.Editor.Focused() || m.input.Focused() {
		t.Fatal("picker paste reached an inactive editor")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if f.picker != nil || !f.Editor.Focused() {
		t.Fatal("closing picker did not return focus to the field")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyF6})
	m.Update(tea.PasteMsg{Content: "hidden edit"})
	if f.Editor.Value() != before || f.Editor.Focused() {
		t.Fatal("category navigation edited a hidden field")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !f.Editor.Focused() {
		t.Fatal("leaving category navigation did not restore field focus")
	}
}
func TestPickerBrowsesNestedFolders(t *testing.T) {
	m := settingsModel(t)
	root := t.TempDir()
	nested := filepath.Join(root, "vendor")
	os.MkdirAll(nested, 0700)
	file := filepath.Join(nested, "model.gguf")
	os.WriteFile(file, nil, 0600)
	m.chat.config.Profile.Provider.Ownership = "managed"
	m.chat.config.Profile.Provider.ModelRoot = root
	m.editSettings("model")
	for i, f := range m.dialog.form.Fields {
		if f.Key == "model-file" {
			m.focusForm(i)
			break
		}
	}
	m.openSettingsPicker()
	p := m.dialog.form.picker
	p.browser = true
	m.acceptSettingsScan(m.scanPickerDirectory(nested)().(settingsScanMsg))
	for i, f := range p.filtered {
		if f == file {
			p.Cursor = i
		}
	}
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog.form.Editor.Value() != file {
		t.Fatal("browser did not select file")
	}
}

func TestSettingsWorkspacePreview(t *testing.T) {
	dir := os.Getenv("TARS_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview opt-in")
	}
	os.MkdirAll(dir, 0700)
	m := settingsModel(t)
	m.chat.config.Profile.Provider.Ownership = "managed"
	m.chat.config.Profile.Provider.ModelFile = "D:/Models/Qwen3-1.7B-Q8_0.gguf"
	m.chat.config.Profile.Provider.GPULayers = 99
	m.width(tea.WindowSizeMsg{Width: 120, Height: 34})
	m.editSettings("model")
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("thinking-workspace")
	for i, f := range m.dialog.form.Fields {
		if f.Key == "model-file" {
			m.focusForm(i)
			break
		}
	}
	m.openSettingsPicker()
	p := m.dialog.form.picker
	m.acceptSettingsScan(settingsScanMsg{p.id, []string{"D:/Models/Qwen3-1.7B-Q8_0.gguf", "E:/LM Studio/models/vendor/Chronos-8B-Q4_K_M.gguf", "E:/LM Studio/models/vendor/Gemma-9B-Q5_K_M.gguf"}, nil})
	write("model-picker")
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.editSettings("generation")
	write("generation-workspace")
	m.width(tea.WindowSizeMsg{Width: 80, Height: 24})
	write("settings-narrow")
}

func TestRemoteModelsDiscovery(t *testing.T) {
	t.Setenv("TARS_TEST_MODEL_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing configured authentication")
		}
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"custom-model"}]}`))
		case "/api/v1/model":
			w.Write([]byte(`{"result":"kobold-model"}`))
		default:
			t.Error("wrong model endpoint", r.URL.Path)
		}
	}))
	defer server.Close()
	for _, kind := range []string{"llama", "openai", "kobold"} {
		models, err := remoteModelChoices(kind, server.URL, "TARS_TEST_MODEL_KEY")
		if err != nil || len(models) != 1 {
			t.Fatalf("%s: %v %v", kind, models, err)
		}
	}
}
func TestSettingsArchiveFailureKeepsActiveConfiguration(t *testing.T) {
	m := settingsModel(t)
	original := m.chat.current
	path := m.stateFile
	// Replace the archive target with a directory to force atomic save failure.
	os.Remove(path)
	os.Mkdir(path, 0700)
	m.editSettings("generation")
	m.dialog.form.Editor.SetValue("0.9")
	m.applyForm()
	if m.dialog == nil || m.dialog.form.error == "" || m.chat.current != original || m.chat.config.Settings.Temperature != 0 {
		t.Fatal("failed persistence changed the active session")
	}
}

func TestSettingsTakesOverContentWithRoundedCorners(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("generation")
	view := m.View().Content
	if strings.Contains(view, "Draft preserved") || strings.Contains(view, "CTX") || strings.Contains(view, "unfinished draft") || strings.Contains(view, "┌") {
		t.Fatal("settings rendered conversation chrome behind its panel")
	}
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╯") {
		t.Fatal("rounded settings corners missing")
	}
	m.closeDialog()
	if m.input.Value() != "unfinished draft" {
		t.Fatal("takeover lost composer draft")
	}
}

func TestSettingsApplyBeforeFirstTurnKeepsLazyConversation(t *testing.T) {
	for _, category := range []string{"model", "generation"} {
		t.Run(category, func(t *testing.T) {
			m := settingsModel(t)
			m.chat.current = nil
			m.stateFile = ""
			m.transcript.blocks = nil
			m.editSettings(category)
			if category == "model" {
				focusSetting(m, "model")
				m.dialog.form.Editor.SetValue("different-model")
			} else {
				m.dialog.form.Editor.SetValue("0.8")
			}
			m.applyForm()
			if m.dialog != nil || m.chat.current != nil || m.stateFile != "" || m.input.Value() != "unfinished draft" {
				t.Fatal("settings fabricated a session or lost draft")
			}
			done := make(chan doneMsg, 1)
			m.send = func(msg tea.Msg) { done <- msg.(doneMsg) }
			if err := m.startConversation("Hello"); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-done:
				if result.err != nil {
					t.Fatal(result.err)
				}
				m.Update(result)
			case <-time.After(time.Second):
				t.Fatal("first turn did not finish")
			}
			archive, err := chat.ReadArchive(m.stateFile)
			if err != nil {
				t.Fatal(err)
			}
			if len(archive.History) != 2 {
				t.Fatal("first turn was not persisted")
			}
		})
	}
}

func TestCharacterAndUserSettingsRemainIndependent(t *testing.T) {
	m := settingsModel(t)
	p := cloneSettingsProfile(m.chat.config.Profile)
	p.Conversation.Character = "character.json"
	p.Conversation.Name = "Fallback"
	p.Conversation.GreetingIndex = 1
	if err := config.ApplySection(&p, "you", map[string]string{"user": "Alex", "persona": "A developer"}); err != nil {
		t.Fatal(err)
	}
	if p.Conversation.Character != "character.json" || p.Conversation.Name != "Fallback" || p.Conversation.GreetingIndex != 1 {
		t.Fatal("user settings changed character")
	}
	if err := config.ApplySection(&p, "character", map[string]string{"character": "", "name": "Ada", "greeting": "0"}); err != nil {
		t.Fatal(err)
	}
	if p.Conversation.UserName != "Alex" || p.Conversation.UserPersona != "A developer" {
		t.Fatal("character settings changed user")
	}
	m.editSettings("character")
	for _, field := range m.dialog.form.Fields {
		if field.Key == "user" || field.Key == "persona" {
			t.Fatal("user fields still mixed with character")
		}
	}
	m.editSettings("you")
	if len(m.dialog.form.Fields) != 2 {
		t.Fatal(m.dialog.form.Fields)
	}
}

func TestPromptPresetIsSharedAndStaged(t *testing.T) {
	for _, agentMode := range []bool{false, true} {
		m := settingsModel(t)
		if agentMode {
			audio := m.chat.config
			m.audioConfig = &audio
			m.chat = nil
		}
		m.editSettings("prompts")
		focusSetting(m, "prompt-preset")
		if m.dialog.form.Editor.Value() != "custom" {
			t.Fatal("existing behavior not preserved")
		}
		options := m.formOptions("prompt-preset")
		if len(options) < 5 || choiceLabel("prompt-preset", "coding") != "Coding" || !strings.Contains(catalogDetail("prompt-preset", "coding"), "maintainable") {
			t.Fatal(options)
		}
		m.dialog.form.Editor.SetValue("coding")
		staged, err := m.stagedSettings()
		if err != nil || staged.Conversation.PromptPreset != "coding" || m.audio().Profile.Conversation.PromptPreset != "" {
			t.Fatal("preset not staged independently", staged, err)
		}
		m.closeDialog()
	}
}
