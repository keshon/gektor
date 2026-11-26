package tui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/settings"
	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
	"github.com/keshon/gektor/pkg/tui/screen"
)

// Staged edits survive a category switch inside one dialog and land
// together on Apply, and the saved task is left for the restart to replace.
func TestSettingsAcrossCategoriesApplyTogether(t *testing.T) {
	m := settingsModel(t)
	path := stateFileOf(m)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	draft := draftOf(m)
	m.editSettings("generation")
	replaceFormText(m, "0.8")
	m.editSettings("you")
	m.focusForm(0)
	replaceFormText(m, "Alex")
	m.editSettings("generation")
	if formSnapshot(m).EditorValue != "0.8" {
		t.Fatal("category switch lost staged edit")
	}
	m.applyForm()
	if m.chrome.DialogInfo().Kind != screen.NoDialog {
		t.Fatalf("apply refused: %q", formSnapshot(m).Error)
	}
	if m.modeChange == nil {
		t.Fatal("apply queued no restart")
	}
	profile := m.modeChange.Preferences.Profile
	if profile.Generation.Temperature != .8 || profile.Identity.UserName != "Alex" {
		t.Fatalf("apply lost staged values: %+v", profile.Generation)
	}
	if draftOf(m) != draft {
		t.Fatal("apply lost the draft")
	}
	if stateFileOf(m) != path {
		t.Fatal("apply moved the session")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("apply rewrote the saved task before the restart landed")
	}
}

func TestSettingsCancelAllCategories(t *testing.T) {
	m := settingsModel(t)
	configured := m.profile
	profile := m.profile.Profile
	m.editSettings("generation")
	replaceFormText(m, "0.9")
	m.editSettings("you")
	m.focusForm(0)
	replaceFormText(m, "Changed")
	m.closeDialog()
	if m.profile != configured || m.profile.Profile.Identity.UserName != profile.Identity.UserName || m.profile.Identity.Temperature != 0 {
		t.Fatal("cancel applied staged values")
	}
	if m.modeChange != nil {
		t.Fatal("cancel queued a restart")
	}
}

func TestPickerSearchBackAndStaleScan(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("profiles")
	for i, f := range formSnapshot(m).Fields {
		if f.Key == "load-profile" {
			m.focusForm(i)
			break
		}
	}
	m.chrome.OpenFormPicker(m.chrome.DialogID())
	p := formSnapshot(m).Picker
	m.Update(fieldform.ScanMsg{ID: p.ID, Items: []string{"C:/profiles/qwen.json", "C:/profiles/chronos.json"}})
	m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if len(formSnapshot(m).Picker.Filtered) != 1 || !strings.Contains(formSnapshot(m).Picker.Filtered[0], "chronos") {
		t.Fatal("search did not narrow profiles")
	}
	previous := formSnapshot(m).EditorValue
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if formSnapshot(m).EditorValue != previous || formSnapshot(m).Picker != nil {
		t.Fatal("picker cancel changed the field")
	}
	m.Update(fieldform.ScanMsg{ID: p.ID, Items: []string{"late.json"}})
	if formSnapshot(m).Picker != nil {
		t.Fatal("late scan reopened picker")
	}
}

func TestSettingsPasteRespectsNestedFocus(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("model")
	for i, field := range formSnapshot(m).Fields {
		if field.Key == "model" {
			m.focusForm(i)
			break
		}
	}
	before := formSnapshot(m).EditorValue
	m.chrome.OpenFormPicker(m.chrome.DialogID())
	p := formSnapshot(m).Picker
	m.Update(fieldform.ScanMsg{ID: p.ID, Items: []string{"llama", "openai", "kobold"}})
	m.Update(tea.PasteMsg{Content: "open"})
	if formSnapshot(m).Picker.Search != "open" || !reflect.DeepEqual(formSnapshot(m).Picker.Filtered, []string{"openai"}) {
		t.Fatal("picker paste did not update search and filtered options")
	}
	if formSnapshot(m).EditorValue != before || formSnapshot(m).EditorFocused || m.View().Cursor == nil {
		t.Fatal("picker paste reached an inactive editor")
	}
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if formSnapshot(m).Picker.Cursor != 1 {
		t.Fatal("picker navigation did not select the second option")
	}
	m.Update(tea.KeyReleaseMsg{Code: tea.KeyDown})
	if formSnapshot(m).Picker.Cursor != 1 {
		t.Fatal("key release reset picker selection without changing its query")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if formSnapshot(m).Picker != nil || !formSnapshot(m).EditorFocused {
		t.Fatal("closing picker did not return focus to the field")
	}
	// A closed picker hands typing back to the field rather than swallowing
	// it or dropping focus.
	m.Update(tea.PasteMsg{Content: "more text"})
	if formSnapshot(m).EditorValue == before || !formSnapshot(m).EditorFocused {
		t.Fatal("pasting after a picker did not reach the field")
	}
}

func TestRemoteModelsDiscovery(t *testing.T) {
	t.Setenv("GEKTOR_TEST_MODEL_KEY", "test-key")
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
		m := settingsModel(t)
		m.editSettings("model")
		snapshot := formSnapshot(m)
		modelIndex := 0
		for i := range snapshot.Fields {
			field := &snapshot.Fields[i]
			switch field.Key {
			case "kind":
				field.Value = kind
			case "endpoint":
				field.Value = server.URL
			case "auth":
				field.Value = "environment"
			case "key-env":
				field.Value = "GEKTOR_TEST_MODEL_KEY"
			case "model":
				modelIndex = i
			}
		}
		presentation := m.preferences.Prepare(snapshot)
		presentation.Values = snapshot.Values()
		m.chrome.ConfigureForm(m.chrome.DialogID(), presentation)
		m.focusForm(modelIndex)
		requestCommand := m.chrome.OpenFormPicker(m.chrome.DialogID())
		request := requestCommand().(fieldform.DiscoveryRequest)
		resultCommand := m.discoverSettings(request)
		result := resultCommand().(fieldform.ScanMsg)
		m.Update(result)
		if result.Err != nil || len(formSnapshot(m).Picker.Items) != 1 {
			t.Fatalf("%s: %v %v", kind, formSnapshot(m).Picker.Items, result.Err)
		}
	}
}

// Apply reads the saved task before it writes anything. A read failure
// must stop the apply outright rather than queue a restart that would
// seed the new session from nothing.
func TestUnreadableTaskStopsApplyBeforeWriting(t *testing.T) {
	m := settingsModel(t)
	configured := m.profile
	path := stateFileOf(m)
	// Replace the task file with a directory to force the read to fail.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	m.editSettings("generation")
	replaceFormText(m, "0.9")
	m.applyForm()
	if m.chrome.DialogInfo().Kind == screen.NoDialog || formSnapshot(m).Error == "" {
		t.Fatal("an unreadable task did not stop the apply")
	}
	if m.modeChange != nil {
		t.Fatal("a failed read still queued a restart")
	}
	if m.profile != configured || m.profile.Identity.Temperature != 0 {
		t.Fatal("failed persistence changed the active configuration")
	}
}

// A settings section is drawn the way every other dialog is drawn: on the
// shared rounded surface, inside the app's own chrome. It used to short-
// circuit the screen and build a bare one - no header, no rules, no side
// borders - so the one screen you open to change how the app looks was the
// only one that did not look like the app.
func TestSettingsIsDrawnInsideTheAppChrome(t *testing.T) {
	m := settingsModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.editSettings("generation")
	view := m.View().Content
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╯") {
		t.Fatalf("rounded settings corners missing:\n%s", view)
	}
	// The header and the action bar the rest of the app has.
	for _, chrome := range []string{"Session", "Help"} {
		if !strings.Contains(view, chrome) {
			t.Fatalf("settings dropped the %q header:\n%s", chrome, view)
		}
	}
	// The shared panel must cover its own rectangle, with history retained outside.
	x, y := m.chrome.FormOrigin(m.chrome.DialogID())
	_, _, width, height := m.formRows()
	lines := strings.Split(view, "\n")
	for _, line := range lines[y:min(y+height, len(lines))] {
		panel := ansi.Cut(line, x, x+width)
		for _, behind := range []string{"Base rules", "hello", "reply"} {
			if strings.Contains(panel, behind) {
				t.Fatalf("conversation showed through form: %q", behind)
			}
		}
	}
	m.closeDialog()
	if draftOf(m) != "unfinished draft" {
		t.Fatal("closing settings lost the composer draft")
	}
}

// Staging a preset must not touch the live profile: the preset is an
// application decision, not a reason to reconfigure services mid-form.
func TestPromptPresetIsStagedNotApplied(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("prompts")
	focusSetting(m, "prompt-preset")
	if formSnapshot(m).EditorValue != "custom" {
		t.Fatal("existing behavior not preserved")
	}
	options := m.chrome.FormOptions(m.chrome.DialogID(), "prompt-preset")
	if len(options) < 5 || settings.ChoiceLabel("prompt-preset", "coding") != "Coding" || !strings.Contains(settings.CatalogDetail("prompt-preset", "coding"), "maintainable") {
		t.Fatal(options)
	}
	replaceFormText(m, "coding")
	staged, err := m.stagedSettings()
	if err != nil || staged.Identity.PromptPreset != "coding" || m.profile.Profile.Identity.PromptPreset != "" {
		t.Fatal("preset not staged independently", staged, err)
	}
	m.closeDialog()
}
