package agent

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/composer"
	"github.com/keshon/gektor/internal/tui/screen"
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
	ta.Prompt = "> "
	ta.SetVirtualCursor(false)
	style := ta.Styles()
	style.Cursor.Shape = tea.CursorUnderline
	ta.SetStyles(inputStyles(style))
	ta.MaxHeight = 6
	ta.DynamicHeight = true
	ta.MaxContentHeight = 4096
	// Line numbers default on in bubbles and render as a phantom "1"
	// in the empty box — it looks like content but submits nothing.
	ta.ShowLineNumbers = false
	return composer.New(ta)
}

func inputStyles(s textarea.Styles) textarea.Styles       { return screen.InputStyles(s) }
func textInputStyles(s textinput.Styles) textinput.Styles { return screen.TextInputStyles(s) }
