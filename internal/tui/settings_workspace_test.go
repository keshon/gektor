package tui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/tui/settings"
)

// Staged edits survive a category switch inside one dialog and land
// together on Apply, and the saved task is left for the restart to replace.
func TestSettingsAcrossCategoriesApplyTogether(t *testing.T) {
	m := settingsModel(t)
	path := m.stateFile
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
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
	if m.dialog != nil {
		t.Fatalf("apply refused: %q", m.dialog.form.Error)
	}
	if m.modeChange == nil {
		t.Fatal("apply queued no restart")
	}
	profile := m.modeChange.Preferences.Profile
	if profile.Generation.Temperature != .8 || profile.Identity.UserName != "Alex" {
		t.Fatalf("apply lost staged values: %+v", profile.Generation)
	}
	if m.input.Value() != draft {
		t.Fatal("apply lost the draft")
	}
	if m.stateFile != path {
		t.Fatal("apply moved the session")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("apply rewrote the saved task before the restart landed")
	}
}

func TestSettingsCancelAllCategories(t *testing.T) {
	m := settingsModel(t)
	configured := m.config
	profile := m.config.Profile
	m.editSettings("generation")
	m.dialog.form.Editor.SetValue("0.9")
	m.editSettings("you")
	m.focusForm(0)
	m.dialog.form.Editor.SetValue("Changed")
	m.closeDialog()
	if m.config != configured || m.config.Profile.Identity.UserName != profile.Identity.UserName || m.config.Identity.Temperature != 0 {
		t.Fatal("cancel applied staged values")
	}
	if m.modeChange != nil {
		t.Fatal("cancel queued a restart")
	}
}
func TestPickerSearchBackAndStaleScan(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("profiles")
	for i, f := range m.dialog.form.Fields {
		if f.Key == "load-profile" {
			m.focusForm(i)
			break
		}
	}
	m.openSettingsPicker()
	p := m.dialog.form.Picker
	m.acceptSettingsScan(settingsScanMsg{ID: p.ID, Items: []string{"C:/profiles/qwen.json", "C:/profiles/chronos.json"}})
	m.pickerKey(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m.pickerKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if len(p.Filtered) != 1 || !strings.Contains(p.Filtered[0], "chronos") {
		t.Fatal("search did not narrow profiles")
	}
	previous := m.dialog.form.Editor.Value()
	m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog.form.Editor.Value() != previous || m.dialog.form.Picker != nil {
		t.Fatal("picker cancel changed the field")
	}
	m.acceptSettingsScan(settingsScanMsg{ID: p.ID, Items: []string{"late.json"}})
	if m.dialog.form.Picker != nil {
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
	p := f.Picker
	p.Items = []string{"llama", "openai", "kobold"}
	p.Filter()
	m.Update(tea.PasteMsg{Content: "open"})
	if p.Search.Value() != "open" || !reflect.DeepEqual(p.Filtered, []string{"openai"}) {
		t.Fatal("picker paste did not update search and filtered options")
	}
	if f.Editor.Value() != before || f.Editor.Focused() || m.input.Focused() {
		t.Fatal("picker paste reached an inactive editor")
	}
	p.Search.SetValue("")
	p.Filter()
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if p.Cursor != 1 {
		t.Fatal("picker navigation did not select the second option")
	}
	m.Update(tea.KeyReleaseMsg{Code: tea.KeyDown})
	if p.Cursor != 1 {
		t.Fatal("key release reset picker selection without changing its query")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if f.Picker != nil || !f.Editor.Focused() {
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
func TestSettingsWorkspacePreview(t *testing.T) {
	dir := os.Getenv("TARS_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview opt-in")
	}
	os.MkdirAll(dir, 0700)
	m := settingsModel(t)
	m.config.Profile.Provider.URL = "http://127.0.0.1:8080"
	m.width(tea.WindowSizeMsg{Width: 120, Height: 34})
	m.editSettings("model")
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("model-page")
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
		models, err := settings.RemoteModelChoices(kind, server.URL, "TARS_TEST_MODEL_KEY")
		if err != nil || len(models) != 1 {
			t.Fatalf("%s: %v %v", kind, models, err)
		}
	}
}
// Apply reads the saved task before it writes anything. A read failure
// must stop the apply outright rather than queue a restart that would
// seed the new session from nothing.
func TestUnreadableTaskStopsApplyBeforeWriting(t *testing.T) {
	m := settingsModel(t)
	configured := m.config
	path := m.stateFile
	// Replace the task file with a directory to force the read to fail.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	m.editSettings("generation")
	m.dialog.form.Editor.SetValue("0.9")
	m.applyForm()
	if m.dialog == nil || m.dialog.form.Error == "" {
		t.Fatal("an unreadable task did not stop the apply")
	}
	if m.modeChange != nil {
		t.Fatal("a failed read still queued a restart")
	}
	if m.config != configured || m.config.Identity.Temperature != 0 {
		t.Fatal("failed persistence changed the active configuration")
	}
}

func TestSettingsTakesOverContentWithRoundedCorners(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("generation")
	view := m.View().Content
	if strings.Contains(view, "Draft preserved") || strings.Contains(view, "CTX") || strings.Contains(view, "unfinished draft") || strings.Contains(view, "┌") {
		t.Fatal("settings rendered Identity chrome behind its panel")
	}
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╯") {
		t.Fatal("rounded settings corners missing")
	}
	m.closeDialog()
	if m.input.Value() != "unfinished draft" {
		t.Fatal("takeover lost composer draft")
	}
}
// Staging a preset must not touch the live profile: the preset is an
// application decision, not a reason to reconfigure services mid-form.
func TestPromptPresetIsStagedNotApplied(t *testing.T) {
	m := settingsModel(t)
		m.editSettings("prompts")
	focusSetting(m, "prompt-preset")
	if m.dialog.form.Editor.Value() != "custom" {
		t.Fatal("existing behavior not preserved")
	}
	options := m.formOptions("prompt-preset")
	if len(options) < 5 || settings.ChoiceLabel("prompt-preset", "coding") != "Coding" || !strings.Contains(settings.CatalogDetail("prompt-preset", "coding"), "maintainable") {
		t.Fatal(options)
	}
	m.dialog.form.Editor.SetValue("coding")
	staged, err := m.stagedSettings()
	if err != nil || staged.Identity.PromptPreset != "coding" || m.config.Profile.Identity.PromptPreset != "" {
		t.Fatal("preset not staged independently", staged, err)
	}
	m.closeDialog()
}
