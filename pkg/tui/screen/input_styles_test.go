package screen_test

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestNativeInputsShareSelectedCaret(t *testing.T) {
	previous := screen.CurrentPalette()
	defer func() {
		screen.SetTheme(previous.Name)
		screen.SetModern(previous.Modern)
		screen.SetCaret(previous.Caret)
	}()
	for _, modern := range []bool{false, true} {
		screen.SetModern(modern)
		for name, shape := range map[string]tea.CursorShape{"": tea.CursorBar, "bar": tea.CursorBar, "block": tea.CursorBlock, "underline": tea.CursorUnderline} {
			screen.SetCaret(name)
			screen.SetTheme("dark")
			screen.SetModern(modern)
			input := textinput.New()
			input.SetVirtualCursor(false)
			input.SetStyles(screen.TextInputStyles(input.Styles()))
			input.Focus()
			editor := textarea.New()
			editor.SetVirtualCursor(false)
			editor.SetStyles(screen.InputStyles(editor.Styles()))
			editor.Focus()
			for _, caret := range []*tea.Cursor{input.Cursor(), editor.Cursor()} {
				if caret == nil || caret.Shape != shape || !caret.Blink {
					t.Fatal("input caret differs", modern, name, caret)
				}
			}
		}
	}
}
