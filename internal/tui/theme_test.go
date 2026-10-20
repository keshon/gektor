package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/keshon/gektor/internal/config"
)

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
	m.dialog.form.Editor.SetValue("light")
	m.applyForm()
	p, err := config.LoadPreferences(path)
	after, err2 := os.ReadFile(m.stateFile)
	if err != nil || err2 != nil || p.Theme != "light" || currentPalette().name != "light" || m.modeChange != nil || m.input.Value() != draft || string(before) != string(after) {
		t.Fatal("theme changed the session or did not persist")
	}
	if themeColor(colorAccent) != themeColor(colorUser) || themeColor(colorDim) != themeColor(colorSecondaryText) {
		t.Fatal("semantic roles disagree")
	}
}

func TestTypingKeepsExplicitScreenColors(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range []string{"light", "dark"} {
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
			if currentPalette().name != theme {
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
	if !sameColor(line[1].Style.Bg, currentPalette().background) || !sameColor(line[3].Style.Bg, currentPalette().background) {
		t.Fatal("reset did not restore screen background")
	}
	if !sameColor(line[2].Style.Bg, "#010203") || sameColor(line[0].Style.Fg, themeColor(colorPopupText)) {
		t.Fatal("screen paint overwrote popup or semantic colors")
	}
}

func TestThemeSetsRendererDefaultsForUnstyledCells(t *testing.T) {
	defer setTheme("dark")
	for _, name := range []string{"dark", "light"} {
		setTheme(name)
		m := settingsModel(t)
		view := m.View()
		if view.BackgroundColor == nil || view.ForegroundColor == nil {
			t.Fatal("renderer inherits terminal colors")
		}
		r, g, b, a := view.BackgroundColor.RGBA()
		er, eg, eb, ea := lipgloss.Color(currentPalette().background).RGBA()
		if r != er || g != eg || b != eb || a != ea {
			t.Fatal("wrong frame background")
		}
		r, g, b, a = view.ForegroundColor.RGBA()
		er, eg, eb, ea = lipgloss.Color(themeColor(colorPopupText)).RGBA()
		if r != er || g != eg || b != eb || a != ea {
			t.Fatal("unstyled text uses terminal foreground")
		}
	}
}
