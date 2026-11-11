package agent

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/keshon/gektor/internal/config"
)

func TestWindowFocusLeavesTerminalColorsAndDraftAlone(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		m := sizeModel(t, testModel())
		m.state = stDone
		m.applyTheme(theme)
		m.input.SetValue("unfinished\nsecond line")
		m.pendingCommands = nil
		m.testUpdate(tea.BlurMsg{})
		_, cmd := m.testUpdate(tea.FocusMsg{})
		if cmd != nil {
			t.Fatal("focus must not override terminal colors")
		}
		if m.input.Value() != "unfinished\nsecond line" || !m.input.Focused() {
			t.Fatal("window focus changed the draft or its input owner")
		}
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
			m.testUpdate(key)
			screen := uv.NewScreenBuffer(m.termW, m.termH)
			uv.NewStyledString(m.testView().Content).Draw(screen, uv.Rect(0, 0, m.termW, m.termH))
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
		view := m.testView()
		if view.BackgroundColor != nil || view.ForegroundColor != nil {
			t.Fatal("theme overrides terminal defaults")
		}
	}
}
