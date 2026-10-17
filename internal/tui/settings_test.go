package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/app"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
)

// headerRowFor finds a menu row by its command, so a test can select one
// without depending on how many rows sit above it.
func headerRowFor(m *model, command string) int {
	for i, item := range m.headerItems() {
		if item.command == command {
			return i
		}
	}
	return -1
}

// scriptClient was conversationClient, renamed when the Chat tests went. The
// settings fixture still needs a scripted llm.Client so Configure can hand
// back services without contacting a backend.
type scriptClient func(context.Context, llm.ChatRequest) (llm.ChatResponse, error)

func (f scriptClient) Chat(ctx context.Context, r llm.ChatRequest) (llm.ChatResponse, error) {
	return f(ctx, r)
}

// settingsModel builds an Agent-shaped session: a configured service set,
// an Application, and a saved task on disk with history.
//
// It used to build a Chat Identity instead, because that was the only
// settings path with an in-place apply. Settings now restart the launch, so
// the fixture has to carry an Application and a real workspace directory or
// requestProfileChange refuses before it reaches anything worth asserting.
func settingsModel(t *testing.T) *model {
	t.Helper()
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("unfinished draft")
	profile := config.Profile{Version: 1, Provider: config.Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local", Ownership: "external"}, Generation: config.Generation{MaxTokens: 100, ContextLimit: 1000}, Identity: &config.Identity{UserName: "User", SystemPrompt: "Base rules"}}
	client := scriptClient(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{Message: llm.Message{Content: "reply"}}, nil
	})
	sessions := t.TempDir()
	workspace := t.TempDir()
	id, _, err := app.Compile(app.IdentityOptions{Preset: profile.Identity.PromptPreset, Name: profile.Identity.Name, Persona: profile.Identity.UserPersona, User: profile.Identity.UserName, System: profile.Identity.SystemPrompt, Post: profile.Identity.PostHistory, MaxTokens: profile.Generation.MaxTokens, ContextLimit: profile.Generation.ContextLimit, Temperature: profile.Generation.Temperature})
	if err != nil {
		t.Fatal(err)
	}
	m.config = &app.Services{Client: client, Identity: id, Directory: sessions, Profile: profile}
	m.application = &Application{
		Preferences:    config.Preferences{Version: 1, Workspace: workspace},
		PreferencePath: filepath.Join(t.TempDir(), "application.json"),
	}
	m.stateFile = filepath.Join(sessions, "original", "state.json")
	m.sessionTitle = "Original"
	if err := os.MkdirAll(filepath.Dir(m.stateFile), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := []llm.Message{
		{Role: llm.RoleSystem, Content: "Base rules"},
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "reply"},
	}
	if err := agent.SaveState(m.stateFile, saved); err != nil {
		t.Fatal(err)
	}
	return m
}
func TestSettingsDirectEditorCancelAndFocus(t *testing.T) {
	m := settingsModel(t)
	configured := m.config
	m.header.global = true
	m.header.focused = true
	m.header.menu = "settings"
	// Resolve the row by command: the menu loses and gains rows whenever a
	// category is added or removed, and a hardcoded index silently selects
	// the wrong one.
	m.header.row = headerRowFor(m, "edit:you")
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
	if m.config != configured || m.input.Value() != "unfinished draft" {
		t.Fatal("form input leaked into session actions")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog != nil || m.config != configured || m.input.Value() != "unfinished draft" || m.config.Profile.Identity.UserName != "User" {
		t.Fatal("cancel mutated configuration or draft")
	}
}

// Apply queues a restart rather than reconfiguring in place, so the saved
// task must be untouched until the new launch replaces it, and the draft
// must survive the handoff.
func TestSettingsValidateThenApplyLeavesTheSavedTaskAlone(t *testing.T) {
	m := settingsModel(t)
	path := m.stateFile
	before, _ := os.ReadFile(path)
	m.editSettings("generation")
	if m.dialog.form.Editor.Value() != "0" {
		t.Fatal("initial temperature lost")
	}
	m.dialog.form.Editor.SetValue("bad")
	m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.dialog == nil || m.dialog.form.Error == "" || m.modeChange != nil {
		t.Fatal("invalid settings applied")
	}
	m.dialog.form.Editor.SetValue("0.8")
	m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	after, _ := os.ReadFile(path)
	if m.dialog != nil {
		t.Fatalf("apply refused: %q", m.dialog.form.Error)
	}
	if m.modeChange == nil {
		t.Fatal("apply queued no restart")
	}
	if got := m.modeChange.Preferences.Profile.Generation.Temperature; got != .8 {
		t.Fatalf("temperature = %v", got)
	}
	if m.stateFile != path || string(before) != string(after) {
		t.Fatal("apply rewrote or moved the saved task before the restart landed")
	}
	if m.input.Value() != "unfinished draft" {
		t.Fatal("apply lost the draft")
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
		t.Fatal("default action deleted Identity")
	}
	m.deleteActive()
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, err := os.Stat(path); !os.IsNotExist(err) || m.sess != nil {
		t.Fatal("explicit delete failed")
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

// An idle session with a live services handle must offer every Settings
// row. The launch without -profile built neither, which disabled the
// whole menu at once and left no hint of which part was missing.
func TestIdleSettingsMenuIsFullyEnabled(t *testing.T) {
	m := settingsModel(t)
	m.header.menu = "settings"
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
	m := settingsModel(t)
	m.header.menu = "settings"
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
	// While a run is in flight, Plan and Act are disabled, so the first
	// row the arrow key can land on is "Stop run" at index 2.
	m.state = stRunning
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

// A choice field toggles with space and the arrow keys rather than
// requiring the reader to type the underlying value.
func TestPromptSwitchCanToggleWithoutTypingBoolean(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("prompts")
	focusSetting(m, "prompt-preset")
	if m.dialog.form.Editor.Value() != "custom" {
		t.Fatal("wrong initial switch")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyDown})
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog.form.Editor.Value() == "custom" {
		t.Fatal("space did not toggle prompt switch")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.config.Profile.Identity.PromptPreset != "" {
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
	m.config.Profile.Provider.ModelRoot = root
	m.config.Profile.Provider.Ownership = "managed"
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
