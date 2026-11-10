package agent

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/settings"
)

func TestEveryThemeCanBeChosenSavedAndReopened(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		t.Run(theme, func(t *testing.T) {
			m := settingsModel(t)
			m.application = &Application{PreferencePath: filepath.Join(t.TempDir(), "application.json")}
			m.command("/theme " + theme)
			p, err := config.LoadPreferences(m.application.PreferencePath)
			if err != nil || p.Theme != theme || currentPalette().Name != theme {
				t.Fatalf("command did not save and apply %s: %+v, %v", theme, p, err)
			}
			m.openTheme()
			if m.chrome.Dialog.Form.Editor.Value() != theme {
				t.Fatal("theme form lost the saved selection")
			}
			if settings.ChoiceLabel("theme", theme) == theme {
				t.Fatal("picker exposes a storage identifier instead of a readable name")
			}
		})
	}
}

func TestWindowFocusLeavesTerminalColorsAndDraftAlone(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		m := sizeModel(t, testModel())
		m.state = stDone
		m.applyTheme(theme)
		m.input.SetValue("unfinished\nsecond line")
		m.pendingCommands = nil
		m.Update(tea.BlurMsg{})
		_, cmd := m.Update(tea.FocusMsg{})
		if cmd != nil {
			t.Fatal("focus must not override terminal colors")
		}
		if m.input.Value() != "unfinished\nsecond line" || !m.input.Focused() {
			t.Fatal("window focus changed the draft or its input owner")
		}
	}
}

func TestThemeChangesOnlyAppearanceAndPersists(t *testing.T) {
	defer setTheme("dark")
	m := settingsModel(t)
	profile := m.config.Profile
	path := filepath.Join(t.TempDir(), "application.json")
	original := config.Preferences{Version: 1, Profile: &profile}
	if err := config.SavePreferences(path, original); err != nil {
		t.Fatal(err)
	}
	m.application = &Application{Preferences: original, PreferencePath: path}
	before, err := os.ReadFile(m.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	draft := m.input.Value()
	m.openTheme()
	m.chrome.Dialog.Form.Editor.SetValue("light")
	m.applyForm()
	p, err := config.LoadPreferences(path)
	after, err2 := os.ReadFile(m.stateFile)
	if err != nil || err2 != nil || p.Theme != "light" || currentPalette().Name != "light" || m.modeChange != nil || m.input.Value() != draft || string(before) != string(after) {
		t.Fatal("theme changed the session or did not persist")
	}
	if themeColor(colorAccent) != themeColor(colorUser) || themeColor(colorDim) != themeColor(colorSecondaryText) {
		t.Fatal("semantic roles disagree")
	}
}

func TestInlineThemeChangesStayInDraftUntilApply(t *testing.T) {
	defer setTheme("dark")
	setTheme("dark")
	m := settingsModel(t)
	path := filepath.Join(t.TempDir(), "application.json")
	p := config.Preferences{Version: 1, Theme: "dark"}
	if err := config.SavePreferences(path, p); err != nil {
		t.Fatal(err)
	}
	m.application = &Application{PreferencePath: path, Preferences: p}
	m.openTheme()
	m.formKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.chrome.Dialog.Form.Picker != nil || m.chrome.Dialog.Form.Editor.Value() != "light" || currentPalette().Name != "dark" {
		t.Fatal("inline choice opened another modal or applied before confirmation")
	}
	m.formKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	saved, err := config.LoadPreferences(path)
	if err != nil || saved.Theme != "dark" {
		t.Fatal("cancel saved the pending theme")
	}
	m.openTheme()
	m.formKey(tea.KeyPressMsg{Code: tea.KeyRight})
	m.formKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	saved, err = config.LoadPreferences(path)
	if err != nil || saved.Theme != "light" || currentPalette().Name != "light" || m.chrome.Dialog != nil {
		t.Fatal("Apply did not save and apply the staged choice")
	}
}

func TestTypingKeepsExplicitScreenColors(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		m := sizeModel(t, testModel())
		m.state = stDone
		m.applyTheme(theme)
		m.appendBlock(answerBlock("A response with **emphasis**."))
		for _, key := range []tea.Msg{
			tea.KeyPressMsg{Code: 'h', Text: "hello"},
			tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift},
			tea.PasteMsg{Content: "another line"},
		} {
			m.Update(key)
			screen := uv.NewScreenBuffer(m.termW, m.termH)
			uv.NewStyledString(m.View().Content).Draw(screen, uv.Rect(0, 0, m.termW, m.termH))
			for y := range m.termH {
				for x := range m.termW {
					cell := screen.CellAt(x, y)
					if cell == nil || cell.Style.Bg == nil || cell.Style.Fg == nil {
						t.Fatalf("%s: input repaint depends on terminal defaults at %d,%d", theme, x, y)
					}
				}
			}
			if currentPalette().Name != theme {
				t.Fatal("input changed theme")
			}
		}
	}
}

func TestScreenPaintPreservesPopupAndSemanticColors(t *testing.T) {
	defer setTheme("dark")
	setTheme("light")
	input := "\x1b[31mR\x1b[mX\x1b[48;2;1;2;3mP\x1b[49mY"
	screen := uv.NewScreenBuffer(4, 1)
	uv.NewStyledString(paintScreen(input)).Draw(screen, uv.Rect(0, 0, 4, 1))
	line := []uv.Cell{*screen.CellAt(0, 0), *screen.CellAt(1, 0), *screen.CellAt(2, 0), *screen.CellAt(3, 0)}
	if !sameColor(line[1].Style.Bg, currentPalette().Background) || !sameColor(line[3].Style.Bg, currentPalette().Background) {
		t.Fatal("reset did not restore screen background")
	}
	if !sameColor(line[2].Style.Bg, "#010203") || sameColor(line[0].Style.Fg, themeColor(colorPopupText)) {
		t.Fatal("screen paint overwrote popup or semantic colors")
	}
}

func TestThemeLeavesTerminalDefaultsAlone(t *testing.T) {
	defer setTheme("dark")
	for _, name := range []string{"dark", "light"} {
		setTheme(name)
		m := settingsModel(t)
		view := m.View()
		if view.BackgroundColor != nil || view.ForegroundColor != nil {
			t.Fatal("theme overrides terminal defaults")
		}
	}
}
