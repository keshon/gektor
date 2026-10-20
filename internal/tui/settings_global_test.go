package tui

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
)

func colorHex(c color.Color) string {
	if c == nil {
		return "<unset>"
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// sameColor compares hex case-insensitively: the palette stores "#RRGGBB"
// and lipgloss normalises to lowercase on the way out.
func sameColor(got color.Color, want string) bool {
	return strings.EqualFold(colorHex(got), want)
}

// An unset cursor colour is not "inherit the theme". The renderer emits a
// cursor-colour reset and hands the cell back to the terminal, so the block
// you type into is painted in the terminal's own defaults. It only shows up
// once the field has focus, which is exactly when someone is typing - so the
// composer looks themed until the first keystroke and then does not.

func TestComposerCursorIsThemedInBothPalettes(t *testing.T) {
	defer setTheme("dark")
	for _, name := range []string{"dark", "light"} {
		setTheme(name)
		want := themeColor(colorAccent)

		composer := inputStyles(textarea.DefaultDarkStyles())
		if !sameColor(composer.Cursor.Color, want) {
			t.Fatalf("%s: composer cursor is %s, want %s", name, colorHex(composer.Cursor.Color), want)
		}
		field := textInputStyles(textinput.DefaultDarkStyles())
		if !sameColor(field.Cursor.Color, want) {
			t.Fatalf("%s: text input cursor is %s, want %s", name, colorHex(field.Cursor.Color), want)
		}
	}
}

// The style has to reach the live widgets too. A /theme mid-run repaints
// through applyTheme, which rebuilds these from whatever it was handed.
func TestApplyingAThemeRestylesTheLiveCursor(t *testing.T) {
	defer setTheme("dark")
	m := sizeModel(t, testModel())
	m.width(tea.WindowSizeMsg{Width: 120, Height: 30})

	for _, name := range []string{"light", "dark"} {
		m.applyTheme(name)
		if got := colorHex(m.input.Styles().Cursor.Color); !sameColor(m.input.Styles().Cursor.Color, themeColor(colorAccent)) {
			t.Fatalf("%s: live composer cursor is %s, want %s",
				name, got, themeColor(colorAccent))
		}
		if got := colorHex(m.note.Styles().Cursor.Color); !sameColor(m.note.Styles().Cursor.Color, themeColor(colorAccent)) {
			t.Fatalf("%s: live note cursor is %s, want %s",
				name, got, themeColor(colorAccent))
		}
	}
}

// Preferences describe the installation, not a transcript. A second copy
// beside each saved conversation made a backend change look like it
// belonged to that conversation, and silently not apply to the next one
// opened.

func TestPreferencesAreNotWrittenPerConversation(t *testing.T) {
	root := t.TempDir()
	m := settingsModel(t)
	conversations := m.sessionDirectory()

	m.application.PreferencePath = filepath.Join(root, "application.json")
	if err := os.MkdirAll(filepath.Dir(m.application.PreferencePath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Turns, resizes and every other event that used to persist this.
	for i := 0; i < 3; i++ {
		if err := m.savePreferences(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(conversations, "settings.json")); err == nil {
		t.Fatal("a settings.json was written beside the conversation")
	}
	if _, err := os.Stat(m.application.PreferencePath); err != nil {
		t.Fatalf("preferences were not written at all: %v", err)
	}
}

// Opening a saved conversation must not restore a settings.json left there
// by a previous version. The file is left where it is; it is simply not read.
func TestOpeningAConversationIgnoresItsOldSettingsFile(t *testing.T) {
	m := settingsModel(t)
	dir := m.sessionDirectory()
	m.stateFile = writePlannedSession(t, dir, "plan", []llm.Message{
		{Role: llm.RoleUser, Content: "question"},
		{Role: llm.RoleAssistant, Content: "answer"},
	})
	mine := m.config.Profile.Clone()
	m.application.Preferences.Profile = &mine

	// A different backend and workspace, written the way the old code did.
	stale := config.Preferences{
		Version:   1,
		Workspace: filepath.Join(t.TempDir(), "somewhere-else"),
		Profile: &config.Profile{
			Version: 1,
			Provider: config.Provider{
				Kind: "openai", URL: "http://elsewhere.invalid", Model: "other",
			},
		},
	}
	if err := config.SavePreferences(filepath.Join(dir, "settings.json"), stale); err != nil {
		t.Fatal(err)
	}

	entry := sessionEntry{dir: dir, title: "plan"}
	m.sessions = &sessionsState{entries: []sessionEntry{entry}}
	m.state = stDone
	m.openSelected()
	if m.sess == nil || m.modeChange != nil {
		t.Fatal("opening the conversation failed outright")
	}
	got := m.application.Preferences
	if got.Profile == nil || got.Profile.Provider.URL != m.application.Preferences.Profile.Provider.URL {
		t.Fatalf("restored %+v; preferences came from the conversation, not the installation",
			got.Profile)
	}
	if got.Workspace != m.application.Preferences.Workspace {
		t.Fatalf("restored workspace %q, want the global %q",
			got.Workspace, m.application.Preferences.Workspace)
	}
	// Left alone, not cleaned up: nothing is destroyed.
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil {
		t.Fatalf("the old file was removed: %v", err)
	}
}
