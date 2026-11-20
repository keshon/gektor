package tui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
)

// headerRowFor finds a menu row by its command, so a test can select one
// without depending on how many rows sit above it.
func headerRowFor(m *Model, command string) int {
	for i, item := range m.headerItems() {
		if item.Command == command {
			return i
		}
	}
	return -1
}

func TestSettingsDirectEditorCancelAndFocus(t *testing.T) {
	m := settingsModel(t)
	configured := m.profile
	m.chrome.Header.Global = true
	m.chrome.Header.Focused = true
	m.chrome.Header.Menu = "settings"
	// Resolve the row by command: the menu loses and gains rows whenever a
	// category is added or removed, and a hardcoded index silently selects
	// the wrong one.
	m.chrome.Header.Row = headerRowFor(m, "edit:you")
	m.headerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.chrome.DialogInfo().Kind != screen.FormDialog || m.chrome.Header.Focused || dialogSnapshot(m).Title != "Settings" {
		t.Fatal("settings redirected instead of opening editor")
	}
	m.focusForm(0)
	if formSnapshot(m).EditorValue != "User" {
		t.Fatal("initial field lost")
	}
	m.Update(tea.PasteMsg{Content: "Alex"})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyF4})
	m.handleKey(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if m.profile != configured || draftOf(m) != "unfinished draft" {
		t.Fatal("form input leaked into session actions")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.chrome.DialogInfo().Kind != screen.NoDialog || m.profile != configured || draftOf(m) != "unfinished draft" || m.profile.Profile.Identity.UserName != "User" {
		t.Fatal("cancel mutated configuration or draft")
	}
}

// Apply queues a restart rather than reconfiguring in place, so the saved
// task must be untouched until the new launch replaces it, and the draft
// must survive the handoff.
func TestSettingsValidateThenApplyLeavesTheSavedTaskAlone(t *testing.T) {
	m := settingsModel(t)
	path := stateFileOf(m)
	before, _ := os.ReadFile(path)
	m.editSettings("generation")
	// An unset temperature reads as the number that will actually be used,
	// not as a bare 0 that looks like deterministic sampling.
	if got := formSnapshot(m).EditorValue; !strings.Contains(got, "default") {
		t.Fatalf("unset temperature shows as %q", got)
	}
	replaceFormText(m, "bad")
	m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.chrome.DialogInfo().Kind == screen.NoDialog || formSnapshot(m).Error == "" || m.modeChange != nil {
		t.Fatal("invalid settings applied")
	}
	replaceFormText(m, "0.8")
	m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	after, _ := os.ReadFile(path)
	if m.chrome.DialogInfo().Kind != screen.NoDialog {
		t.Fatalf("apply refused: %q", formSnapshot(m).Error)
	}
	if m.modeChange == nil {
		t.Fatal("apply queued no restart")
	}
	if got := m.modeChange.Preferences.Profile.Generation.Temperature; got != .8 {
		t.Fatalf("temperature = %v", got)
	}
	if stateFileOf(m) != path || string(before) != string(after) {
		t.Fatal("apply rewrote or moved the saved task before the restart landed")
	}
	if draftOf(m) != "unfinished draft" {
		t.Fatal("apply lost the draft")
	}
}

func TestSettingsFormMouseAndFrame(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := settingsModel(t)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m.editSettings("prompts")
		for _, row := range strings.Split(m.View().Content, "\n") {
			if lipgloss.Width(row) != width {
				t.Fatalf("width=%d row=%s", width, ansi.Strip(row))
			}
		}
		// Every drawn row fills the screen, and Cancel is where it is
		// drawn - not where a second copy of the layout maths says.
		clickVisible(t, m, "[ Cancel ]")
		if m.chrome.DialogInfo().Kind != screen.NoDialog || draftOf(m) != "unfinished draft" {
			t.Fatal("mouse cancel missed or lost draft")
		}
	}
}

func TestSettingsMouseResizesMultilineEditor(t *testing.T) {
	m := settingsModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.editSettings("you")
	// A multiline field and a single-line one, named by key so the test does
	// not depend on their order or on a label also appearing in the hint.
	for _, c := range []struct {
		key  string
		want int
	}{
		{"persona", 3},
		{"user", 1},
	} {
		clickHit(t, m, fieldHit(t, m, c.key))
		if formSnapshot(m).EditorHeight != c.want {
			t.Fatalf("clicking %q left the editor at height %d, want %d",
				c.key, formSnapshot(m).EditorHeight, c.want)
		}
		if formSnapshot(m).Fields[fieldHit(t, m, c.key)].Key != c.key {
			t.Fatalf("the click did not focus %q", c.key)
		}
	}
}

// An idle session with a live services handle must offer every Settings
// row. The launch without -profile built neither, which disabled the
// whole menu at once and left no hint of which part was missing.
func TestIdleSettingsMenuIsFullyEnabled(t *testing.T) {
	m := settingsModel(t)
	m.chrome.Header.Menu = "settings"
	rows := m.rawHeaderItems()
	if len(rows) == 0 {
		t.Fatal("settings menu is empty")
	}
	for _, item := range rows {
		if item.Command == "" {
			continue
		}
		if !item.Enabled {
			t.Fatalf("idle Settings row disabled: %q (%s)", item.Text, item.Command)
		}
	}
}

// Each Settings section must actually open. A row that is enabled but
// Enter-closes-without-an-effect is the same bug wearing a different hat:
// editSettings guards on the same services handle the menu did.
func TestEveryIdleSettingsRowOpens(t *testing.T) {
	m := settingsModel(t)
	m.chrome.Header.Menu = "settings"
	for _, item := range m.rawHeaderItems() {
		if item.Text == "" || !item.Enabled {
			continue
		}
		m.closeDialog()
		m.chrome.Header.Focused, m.chrome.Header.Global, m.chrome.Header.Menu = true, true, "settings"
		m.chrome.Header.Row = 0
		for i, candidate := range m.headerItems() {
			if candidate.Command == item.Command {
				m.chrome.Header.Row = i
			}
		}
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.chrome.DialogInfo().Kind == screen.NoDialog {
			t.Fatalf("Settings row %q (%s) opened nothing", item.Text, item.Command)
		}
	}
}

func TestCommonMenusHaveNoRedirects(t *testing.T) {
	m := settingsModel(t)
	for _, category := range []string{"session", "execution", "view", "settings", "help"} {
		m.chrome.Header.Menu = category
		for _, item := range m.rawHeaderItems() {
			if strings.HasPrefix(item.Command, "open:") {
				t.Fatal("menu redirects")
			}
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.agent.Status().Idle {
		t.Fatal("fixture did not start an Agent turn")
	}
	m.chrome.Header.Menu = "settings"
	for _, item := range m.rawHeaderItems() {
		if strings.HasPrefix(item.Command, "edit:") && item.Enabled {
			t.Fatal("settings editable during response")
		}
	}
	m.chrome.Header.Global = true
	m.chrome.Header.Focused = true
	m.chrome.Header.Menu = "execution"
	m.chrome.Header.Row = 0
	// Mode and access choices are disabled while a run is in flight.
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	items := m.headerItems()
	selected := items[m.chrome.Header.Row]
	if selected.Command != "toggle-steering" || !selected.Enabled {
		t.Fatal("keyboard did not skip disabled actions")
	}
	finishAgentTurn(t, m)
}

// A choice field toggles with space and the arrow keys rather than
// requiring the reader to type the underlying value.
func TestPromptSwitchCanToggleWithoutTypingBoolean(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("prompts")
	focusSetting(m, "prompt-preset")
	if formSnapshot(m).EditorValue != "custom" {
		t.Fatal("wrong initial switch")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if formSnapshot(m).EditorValue == "custom" {
		t.Fatal("space did not toggle prompt switch")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.profile.Profile.Identity.PromptPreset != "" {
		t.Fatal("cancel persisted switch")
	}
}

func TestFormArrowNavigationPreservesValues(t *testing.T) {
	m := settingsModel(t)
	m.chrome.OpenForm("Settings", "", "Apply", []form.Field{{Key: "one", Label: "First", Value: "one", Multiline: false}, {Key: "two", Label: "Second", Value: "two", Multiline: false}})
	replaceFormText(m, "edited")
	m.formKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if formSnapshot(m).Index != 1 || formSnapshot(m).Fields[0].Value != "edited" {
		t.Fatal("down lost value or focus")
	}
	m.formKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if formSnapshot(m).Index != 0 || formSnapshot(m).EditorValue != "edited" {
		t.Fatal("up lost value or focus")
	}
}

func TestProtocolSwitchSuggestsOnlyLocalDefaults(t *testing.T) {
	for _, tc := range []struct{ endpoint, previous, next, want string }{
		{"http://127.0.0.1:9931", "llama", "kobold", "http://127.0.0.1:5001"},
		{"http://localhost:5001/", "kobold", "openai", "http://localhost:8080/v1"},
		{"http://localhost:8080/v1", "openai", "llama", "http://localhost:9931"},
		{"http://127.0.0.1:8080", "llama", "kobold", "http://127.0.0.1:8080"},
		{"http://localhost:9000", "llama", "kobold", "http://localhost:9000"},
		{"https://events.openai.com/v1", "openai", "llama", "https://events.openai.com/v1"},
		{"http://localhost:8080/custom", "llama", "kobold", "http://localhost:8080/custom"},
	} {
		if got := settings.SuggestedBackendURL(tc.endpoint, tc.previous, tc.next); got != tc.want {
			t.Fatalf("%s: got %s want %s", tc.endpoint, got, tc.want)
		}
	}
	m := settingsModel(t)
	m.profile.Profile.Provider.URL = "http://127.0.0.1:9931"
	m.editSettings("model")
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.focusForm(1)
	if formSnapshot(m).EditorValue != "http://127.0.0.1:5001" {
		t.Fatal("suggested URL not visible in editor")
	}
	replaceFormText(m, "http://localhost:9000")
	m.focusForm(0)
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.focusForm(1)
	if formSnapshot(m).EditorValue != "http://localhost:9000" {
		t.Fatal("custom URL overwritten")
	}
}
