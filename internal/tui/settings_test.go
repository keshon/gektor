package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/speech"
)

func settingsModel(t *testing.T) *model {
	t.Helper()
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("unfinished draft")
	profile := config.Profile{Version: 1, Provider: config.Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local", Ownership: "external"}, Generation: config.Generation{MaxTokens: 100, ContextLimit: 1000}, Conversation: &config.Conversation{UserName: "User", SystemPrompt: "Base rules"}}
	client := conversationClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{Message: llm.Message{Content: "reply"}}, nil
	})
	var configure func(config.Profile) (ChatConfig, error)
	configure = func(p config.Profile) (ChatConfig, error) {
		if err := p.Validate(); err != nil {
			return ChatConfig{}, err
		}
		if p.Conversation.UserName == "" {
			return ChatConfig{}, fmt.Errorf("user name required")
		}
		return ChatConfig{Client: client, Settings: chat.Config{System: p.Conversation.SystemPrompt, Temperature: p.Generation.Temperature, MaxTokens: p.Generation.MaxTokens, ContextLimit: p.Generation.ContextLimit}, Directory: m.chat.config.Directory, Profile: p, Configure: configure}, nil
	}
	m.chat = &chatUI{config: &ChatConfig{Client: client, Settings: chat.Config{System: "Base rules", MaxTokens: 100, ContextLimit: 1000}, Directory: t.TempDir(), Profile: profile, Configure: configure}}
	c, err := chat.New(client, m.chat.config.Settings)
	if err != nil {
		t.Fatal(err)
	}
	m.chat.current = c
	m.stateFile = filepath.Join(m.chat.config.Directory, "original", "conversation.json")
	m.sessionTitle = "Original"
	if _, err = c.AskAndSave(context.Background(), "hello", m.stateFile); err != nil {
		t.Fatal(err)
	}
	return m
}
func TestSettingsDirectEditorCancelAndFocus(t *testing.T) {
	m := settingsModel(t)
	current := m.chat.current
	m.header.global = true
	m.header.focused = true
	m.header.menu = "settings"
	m.header.row = 6
	m.headerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog == nil || m.dialog.form == nil || m.header.focused || m.dialog.title != "Settings" {
		t.Fatal("settings redirected instead of opening editor")
	}
	m.focusForm(0)
	if m.dialog.form.Editor.Value() != "User" {
		t.Fatal("initial field lost")
	}
	m.Update(tea.PasteMsg{Content: "Alex"})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyF4})
	m.handleKey(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if m.chat.current != current || m.input.Value() != "unfinished draft" {
		t.Fatal("form input leaked into conversation actions")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog != nil || m.chat.current != current || m.input.Value() != "unfinished draft" || m.chat.config.Profile.Conversation.UserName != "User" {
		t.Fatal("cancel mutated configuration or draft")
	}
}
func TestSettingsValidateThenApplyPreservesHistoryAndUpdatesArchive(t *testing.T) {
	m := settingsModel(t)
	path := m.stateFile
	before, _ := os.ReadFile(path)
	m.editSettings("generation")
	if m.dialog.form.Editor.Value() != "0" {
		t.Fatal("initial temperature lost")
	}
	m.dialog.form.Editor.SetValue("bad")
	m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.dialog == nil || m.dialog.form.Error == "" || m.chat.current == nil || m.chat.config.Settings.Temperature != 0 {
		t.Fatal("invalid settings applied")
	}
	m.dialog.form.Editor.SetValue("0.8")
	m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	after, _ := os.ReadFile(path)
	if m.dialog != nil || m.chat.current == nil || m.stateFile != path || m.chat.config.Settings.Temperature != .8 || string(before) == string(after) || m.input.Value() != "unfinished draft" {
		t.Fatalf("apply did not preserve session: dialog=%+v current=%v path=%s temp=%v before=%s after=%s draft=%s", m.dialog, m.chat.current != nil, m.stateFile, m.chat.config.Settings.Temperature, before, after, m.input.Value())
	}
}
func TestDirectRenameAndDeleteDefaultCancel(t *testing.T) {
	m := settingsModel(t)
	path := m.stateFile
	m.renameActive()
	m.dialog.form.Editor.SetValue("New title")
	m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.dialog != nil || m.sessionTitle != "New title" {
		t.Fatal("rename failed")
	}
	m.deleteActive()
	m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.dialog == nil {
		t.Fatal("delete bypassed default cancel")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, err := os.Stat(path); err != nil {
		t.Fatal("default action deleted conversation")
	}
	m.deleteActive()
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, err := os.Stat(path); !os.IsNotExist(err) || m.chat.current != nil {
		t.Fatal("explicit delete failed")
	}
}
func TestSaveProfileRefusesOverwriteAndKeepsCardPath(t *testing.T) {
	m := settingsModel(t)
	root := t.TempDir()
	card := filepath.Join(root, "card.json")
	os.WriteFile(card, []byte(`{"name":"Ada"}`), 0600)
	m.chat.config.Profile.Conversation.Character = card
	first := filepath.Join(root, "existing.json")
	os.WriteFile(first, []byte("original"), 0600)
	m.saveChatProfile()
	m.dialog.form.Editor.SetValue(first)
	m.applyForm()
	if m.dialog == nil || m.dialog.form.Error == "" {
		t.Fatal("existing profile overwritten")
	}
	next := filepath.Join(root, "other folder", "profile.json")
	m.dialog.form.Editor.SetValue(next)
	m.applyForm()
	if m.dialog != nil {
		t.Fatal("save failed")
	}
	p, err := config.Load(next)
	if err != nil {
		t.Fatal(err)
	}
	if p.Conversation.Character != card {
		t.Fatal("retry resolved card against wrong directory")
	}
	data, _ := os.ReadFile(first)
	if string(data) != "original" {
		t.Fatal("original file altered")
	}
}
func TestSettingsFormMouseAndFrame(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := settingsModel(t)
		m.width(tea.WindowSizeMsg{Width: width, Height: 24})
		m.editSettings("prompts")
		for _, row := range strings.Split(m.View().Content, "\n") {
			if lipgloss.Width(row) != width {
				t.Fatalf("width=%d row=%s", width, ansi.Strip(row))
			}
		}
		rows, hits, w, h := m.formRows()
		bw := 0
		for _, row := range rows {
			bw = max(bw, lipgloss.Width(row))
		}
		x := m.conversationOffset() + max((w-bw-4)/2, 0) + 1
		y := paneTop + max((h-len(rows)-2)/2, 0) + 1
		if m.dialog.form.Workspace != nil {
			x = 2
			y = 2
		}
		cancel := hits[len(hits)-1]
		if cancel.X1 > 0 {
			x = m.conversationOffset() + max((w-bw-4)/2, 0) + 2 + cancel.X0
			if m.dialog.form.Workspace != nil {
				x = 2 + cancel.X0
			}
			if m.dialog.form.Workspace != nil && w >= 76 {
				x += 17
			}
		}
		m.Update(tea.MouseClickMsg{X: x, Y: y + cancel.Y0, Button: tea.MouseLeft})
		if m.dialog != nil || m.input.Value() != "unfinished draft" {
			t.Fatal("mouse cancel missed or lost draft")
		}
	}
}

func TestSettingsMouseResizesMultilineEditor(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("you")
	for _, index := range []int{1, 0} {
		_, hits, _, _ := m.formRows()
		for _, hit := range hits {
			if hit.Index != index {
				continue
			}
			m.formMouse(tea.MouseClickMsg{X: 25, Y: 2 + hit.Y0, Button: tea.MouseLeft})
			break
		}
		want := 1
		if index == 1 {
			want = 3
		}
		if m.dialog.form.Index != index || m.dialog.form.Editor.Height() != want {
			t.Fatalf("mouse focus %d left editor at height %d", index, m.dialog.form.Editor.Height())
		}
	}
}
// agentSettingsModel puts the session in the bare `-tui` shape: Agent
// mode with a services handle and an Application, which is what the
// launch builds when no -profile was given.
func agentSettingsModel(t *testing.T) *model {
	t.Helper()
	m := settingsModel(t)
	audio := m.chat.config
	audio.Profile.Speech = &speech.Config{Microphone: "mic", Speaking: speech.Speaking{Provider: "chatterbox"}}
	m.chat = nil
	m.audioConfig = audio
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "agent"}}
	m.header.menu = "settings"
	return m
}

// An idle session with a live services handle must offer every Settings
// row. The launch without -profile built neither, which disabled the
// whole menu at once and left no hint of which part was missing.
func TestIdleSettingsMenuIsFullyEnabled(t *testing.T) {
	m := agentSettingsModel(t)
	rows := m.rawHeaderItems()
	if len(rows) == 0 {
		t.Fatal("settings menu is empty")
	}
	for _, item := range rows {
		if item.text == "" && item.command == "" {
			continue
		}
		if !item.enabled {
			t.Fatalf("idle Settings row disabled: %q (%s)", item.text, item.command)
		}
	}
}

// Each Settings section must actually open. A row that is enabled but
// Enter-closes-without-an-effect is the same bug wearing a different hat:
// editSettings guards on the same services handle the menu did.
func TestEveryIdleSettingsRowOpens(t *testing.T) {
	m := agentSettingsModel(t)
	for _, item := range m.rawHeaderItems() {
		if item.text == "" || !item.enabled {
			continue
		}
		m.closeDialog()
		m.header.focused, m.header.global, m.header.menu = true, true, "settings"
		m.header.row = 0
		for i, candidate := range m.headerItems() {
			if candidate.command == item.command {
				m.header.row = i
			}
		}
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.dialog == nil {
			t.Fatalf("Settings row %q (%s) opened nothing", item.text, item.command)
		}
	}
}

func TestCommonMenusHaveNoRedirects(t *testing.T) {
	m := settingsModel(t)
	for _, category := range []string{"session", "workflow", "view", "settings", "help"} {
		m.header.menu = category
		for _, item := range m.rawHeaderItems() {
			if strings.HasPrefix(item.command, "open:") {
				t.Fatal("menu redirects")
			}
		}
	}
	m.state = stRunning
	m.header.menu = "settings"
	for _, item := range m.rawHeaderItems() {
		if strings.HasPrefix(item.command, "edit:") && item.enabled {
			t.Fatal("settings editable during response")
		}
	}
	m.header.global = true
	m.header.focused = true
	m.header.menu = "workflow"
	m.header.row = 0
	m.state = stDone
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.header.row != 2 {
		t.Fatal("keyboard did not skip disabled actions")
	}
}
func TestMenuSettingsPreview(t *testing.T) {
	dir := os.Getenv("TARS_SETTINGS_PREVIEW_DIR")
	if dir == "" {
		t.Skip("renderer export opt-in")
	}
	os.MkdirAll(dir, 0700)
	for _, name := range []string{"menu", "generation", "prompts", "delete"} {
		m := settingsModel(t)
		m.width(tea.WindowSizeMsg{Width: 100, Height: 28})
		switch name {
		case "menu":
			m.header.global = true
			m.header.focused = true
			m.header.menu = "settings"
		case "delete":
			m.deleteActive()
		default:
			m.editSettings(name)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFormFooterMatchesActionsAndViewIsPure(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("generation")
	before := m.dialog.form.Editor.Value()
	cursor := m.dialog.form.Editor.Column()
	first := m.View().Content
	second := m.View().Content
	if first != second || m.dialog.form.Editor.Value() != before || m.dialog.form.Editor.Column() != cursor {
		t.Fatal("render mutated form")
	}
	for _, action := range m.footerActions() {
		if action.name == "F1" {
			t.Fatal("footer advertises unrelated help action")
		}
	}
	for _, cell := range m.footerCells() {
		if cell.action.name == "Esc" {
			m.Update(tea.MouseClickMsg{X: cell.start, Y: m.termH - 1, Button: tea.MouseLeft})
			break
		}
	}
	if m.dialog != nil {
		t.Fatal("footer cancel click failed")
	}
}

func TestPromptSwitchCanToggleWithoutTypingBoolean(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("prompts")
	focusSetting(m, "prefer-main")
	if m.dialog.form.Editor.Value() != "true" {
		t.Fatal("wrong initial switch")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyDown})
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog.form.Editor.Value() != "false" {
		t.Fatal("space did not toggle prompt switch")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.chat.config.Profile.Conversation.PreferCharacterPrompt != nil {
		t.Fatal("cancel persisted switch")
	}
}

func TestFormArrowNavigationPreservesValues(t *testing.T) {
	m := settingsModel(t)
	m.openForm("Settings", "", "Apply", []formField{{Key: "one", Label: "First", Value: "one", Multiline: false}, {Key: "two", Label: "Second", Value: "two", Multiline: false}}, func(map[string]string) error { return nil })
	m.dialog.form.Editor.SetValue("edited")
	m.formKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.dialog.form.Index != 1 || m.dialog.form.Fields[0].Value != "edited" {
		t.Fatal("down lost value or focus")
	}
	m.formKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.dialog.form.Index != 0 || m.dialog.form.Editor.Value() != "edited" {
		t.Fatal("up lost value or focus")
	}
}

func TestProtocolSwitchSuggestsOnlyLocalDefaults(t *testing.T) {
	for _, tc := range []struct{ endpoint, previous, next, want string }{
		{"http://127.0.0.1:8080", "llama", "kobold", "http://127.0.0.1:5001"},
		{"http://localhost:5001/", "kobold", "openai", "http://localhost:8080/v1"},
		{"http://localhost:8080/v1", "openai", "llama", "http://localhost:8080"},
		{"http://localhost:9000", "llama", "kobold", "http://localhost:9000"},
		{"https://api.openai.com/v1", "openai", "llama", "https://api.openai.com/v1"},
		{"http://localhost:8080/custom", "llama", "kobold", "http://localhost:8080/custom"},
	} {
		if got := suggestedBackendURL(tc.endpoint, tc.previous, tc.next); got != tc.want {
			t.Fatalf("%s: got %s want %s", tc.endpoint, got, tc.want)
		}
	}
	m := settingsModel(t)
	m.openForm("Connection", "", "Apply", []formField{{Key: "kind", Label: "Protocol", Value: "llama", Multiline: false}, {Key: "endpoint", Label: "URL", Value: "http://127.0.0.1:8080", Multiline: false}}, func(map[string]string) error { return nil })
	m.cycleFormChoice()
	m.focusForm(1)
	if m.dialog.form.Editor.Value() != "http://127.0.0.1:5001" {
		t.Fatal("suggested URL not visible in editor")
	}
	m.dialog.form.Editor.SetValue("http://localhost:9000")
	m.focusForm(0)
	m.cycleFormChoice()
	m.focusForm(1)
	if m.dialog.form.Editor.Value() != "http://localhost:9000" {
		t.Fatal("custom URL overwritten")
	}
}

func TestModelFormDiscoversNestedModelChoices(t *testing.T) {
	m := settingsModel(t)
	root := t.TempDir()
	nested := filepath.Join(root, "vendor", "model")
	os.MkdirAll(nested, 0700)
	file := filepath.Join(nested, "weights.gguf")
	os.WriteFile(file, nil, 0600)
	m.chat.config.Profile.Provider.ModelRoot = root
	m.chat.config.Profile.Provider.Ownership = "managed"
	m.editSettings("model")
	for i, field := range m.dialog.form.Fields {
		if field.Key == "model-file" {
			m.focusForm(i)
			cmd := m.openSettingsPicker()
			m.acceptSettingsScan(cmd().(settingsScanMsg))
			m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			if m.dialog.form.Editor.Value() != file {
				t.Fatal("nested model not selectable")
			}
			return
		}
	}
	t.Fatal("missing model-file field")
}
