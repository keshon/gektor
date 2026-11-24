package agent

import (
	"math"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/pkg/tui/components/cell"
	"github.com/keshon/gektor/pkg/tui/components/composer"
	"github.com/keshon/gektor/pkg/tui/screen"
)

// truncate shortens display strings with a marker. Rune-based:
// a byte slice can split multi-byte UTF-8 and emit invalid output.
// Lives in components/cell; this is the root package's spelling of it.
func truncate(s string, n int) string { return cell.Truncate(s, n) }

// newInput builds the answer box: multiline, capped at a few rows.
// Enter submits (handled by the model, never reaching the widget);
// Ctrl+O inserts a newline; Shift+Enter also works when the host preserves it.
func newInput() composer.Model {
	ta := textarea.New()
	ta.Prompt = "❯ "
	ta.SetVirtualCursor(false)
	style := ta.Styles()
	style.Cursor.Shape = tea.CursorBar
	ta.SetStyles(inputStyles(style))
	ta.MaxHeight = 6
	ta.DynamicHeight = true
	// MaxHeight bounds the editor viewport; pasted drafts must not be truncated.
	ta.MaxContentHeight = math.MaxInt
	// Line numbers default on in bubbles and render as a phantom "1"
	// in the empty box — it looks like content but submits nothing.
	ta.ShowLineNumbers = false
	return composer.New(ta)
}

func inputStyles(s textarea.Styles) textarea.Styles {
	s = screen.InputStyles(s)
	s.Cursor.Color = lipgloss.Color(themeColor(colorPopupText))
	return s
}
func textInputStyles(s textinput.Styles) textinput.Styles { return screen.TextInputStyles(s) }
