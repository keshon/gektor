package tui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/config"
)

func TestInspectorShortcutRolesAndCompactFooterAcrossThemes(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		for _, width := range []int{40, 80, 121} {
			m := inspectorModel(t, width)
			m.applyTheme(theme)
			m.openInspector(len(m.transcript.blocks) - 1)
			view := m.View().Content
			area := m.screenPanes(true).Side
			layout := m.panelGeometry(area.Width, 1, true, false)
			x, y := area.X+layout.Footer.X, area.Y+layout.Footer.Y
			screen := uv.NewScreenBuffer(width, m.termH)
			uv.NewStyledString(view).Draw(screen, uv.Rect(0, 0, width, m.termH))
			key, action := screen.CellAt(x, y), screen.CellAt(x+4, y)
			if key == nil || action == nil || !sameColor(key.Style.Fg, themeColor(colorPopupText)) || !sameColor(action.Style.Fg, themeColor(colorDim)) {
				t.Fatal("shortcut key and action roles differ from theme", theme, width)
			}
			row := ansi.Cut(strings.Split(ansi.Strip(view), "\n")[y], x, x+layout.Footer.Width)
			if !strings.HasPrefix(row, "ESC ") || strings.ContainsAny(row, "·…") || layout.Footer.Height != 1 {
				t.Fatal("inspector footer lost its compact whole-group hints", theme, width, row)
			}
		}
	}
}
