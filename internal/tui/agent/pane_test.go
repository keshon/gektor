package agent

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
)

func TestInspectorFooterStaysOneRowWhenComposerGrows(t *testing.T) {
	m := inspectorModel(t, 121)
	m.input.SetValue("one\ntwo\nthree\nfour")
	m.fitInput()
	m.fitBottom()
	m.openInspector(len(m.transcript.blocks) - 1)
	sideHeight := m.inspector.pane.Viewport.Height()
	mainHeight := m.transcript.Viewport.Height()
	inputHeight := m.input.Height()
	rows := strings.Split(ansi.Strip(m.testView().Content), "\n")
	if !strings.Contains(ansi.Cut(rows[26], 60, 121), "────") || !strings.Contains(ansi.Cut(rows[27], 60, 121), "ESC") {
		t.Fatal("inspector footer lacks a divider or occupies extra rows")
	}
	m.input.SetValue("one")
	m.fitInput()
	m.fitBottom()
	if m.inspector.pane.Viewport.Height() != sideHeight {
		t.Fatal("inspector inherited composer height")
	}
	if m.transcript.Viewport.Height() != mainHeight+inputHeight-m.input.Height() {
		t.Fatal("composer resize lost available conversation rows")
	}
}

func TestPaneFocusChangesWithoutMovingFrames(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		m := inspectorModel(t, 121)
		m.applyTheme(theme)
		m.openInspector(len(m.transcript.blocks) - 1)
		for _, focused := range []bool{true, false} {
			if !focused {
				m.testKey(tea.KeyPressMsg{Code: tea.KeyTab})
			}
			screen := uv.NewScreenBuffer(m.termW, m.termH)
			uv.NewStyledString(m.testView().Content).Draw(screen, uv.Rect(0, 0, m.termW, m.termH))
			main, side := screen.CellAt(0, 1), screen.CellAt(60, 1)
			mainColor, sideColor := themeColor(colorPopupBorder), themeColor(colorAccent)
			if !focused {
				mainColor, sideColor = sideColor, mainColor
			}
			if main == nil || side == nil || !sameColor(main.Style.Fg, mainColor) || !sameColor(side.Style.Fg, sideColor) {
				t.Fatalf("%s: focus is not visible on pane borders", theme)
			}
		}
	}
}
