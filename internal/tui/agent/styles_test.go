package agent

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
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
