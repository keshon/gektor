package agent

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

func TestAgentControlsUseSharedShortcutRoles(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		m := sizeModel(t, testModel())
		m.applyTheme(theme)
		for _, state := range []runState{stDone, stAsk, stRunning, stStopping, stPermission} {
			m.state = state
			for _, width := range []int{20, 36, 74} {
				m.transcript.Viewport.SetWidth(width)
				hint := m.actionHint()
				plain := ansi.Strip(hint)
				if ansi.StringWidth(hint) > width || strings.ContainsAny(plain, "…·") || strings.Contains(plain, "Ctrl+") || strings.Contains(plain, "Enter") || strings.Contains(plain, "Esc") {
					t.Fatal("Agent hint bypassed shortcut formatting", theme, state, width, plain)
				}
				if (state == stRunning || state == stAsk) && !strings.HasPrefix(plain, "ESC stop") {
					t.Fatal("narrow active-run hint lost stop action", plain)
				}
				buffer := uv.NewScreenBuffer(width, 1)
				uv.NewStyledString(paintScreen(hint)).Draw(buffer, uv.Rect(0, 0, width, 1))
				if !sameColor(buffer.CellAt(0, 0).Style.Fg, themeColor(colorPopupText)) {
					t.Fatal("Agent shortcut key lost theme role", theme, state, width)
				}
			}
		}
	}
}

func TestCustomDialogShortcutsKeepWholeActions(t *testing.T) {
	m := sizeModel(t, testModel())
	m.testControls().Open("Review proposal", []string{"Proposed changes"})
	m.chrome.Dialog.Shortcuts = []popup.Shortcut{{Key: "Esc/Enter", Action: "discard"}, {Key: "S", Action: "apply to draft"}}
	for _, width := range []int{40, 80} {
		sizeModelTo(t, m, width, 24)
		hint := ansi.Strip(m.testControls().LayoutDialog().Hint)
		if !strings.Contains(hint, "ESC/ENTER discard") || strings.Contains(hint, "save lesson") || strings.ContainsAny(hint, "…·") {
			t.Fatal("custom dialog hint bypassed shared renderer", width, hint)
		}
		if width == 80 && !strings.Contains(hint, "S apply to draft") {
			t.Fatal("custom dialog lost its primary action", hint)
		}
	}
}

func TestInspectorShortcutRolesAndCompactFooterAcrossThemes(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		for _, width := range []int{40, 80, 121} {
			m := inspectorModel(t, width)
			m.applyTheme(theme)
			m.openInspector(len(m.transcript.blocks) - 1)
			view := m.testView().Content
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
