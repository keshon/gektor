package tui

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/keshon/tars/internal/tui/components/cell"
	"github.com/keshon/tars/internal/tui/components/composer"
)

// truncate shortens display strings with a marker. Rune-based:
// a byte slice can split multi-byte UTF-8 and emit invalid output.
// Lives in components/cell; this is the root package's spelling of it.
func truncate(s string, n int) string { return cell.Truncate(s, n) }

// newInput builds the answer box: multiline, capped at a few rows.
// Enter submits (handled by the model, never reaching the widget);
// Shift+Enter inserts a newline; Ctrl+O remains a terminal fallback.
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

func inputStyles(styles textarea.Styles) textarea.Styles {
	base := lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorPopupText))).Background(lipgloss.Color(currentPalette().background))
	state := textarea.StyleState{Base: base, Text: base, CursorLine: base, Prompt: lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorAccent))), Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorDim))), EndOfBuffer: base}
	_, _, selected := popupStyles()
	state.Selection = selected
	styles.Focused = state
	styles.Blurred = state
	return styles
}

func textInputStyles(styles textinput.Styles) textinput.Styles {
	state := textinput.StyleState{Text: lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorPopupText))), Prompt: lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorAccent))), Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorDim))), Suggestion: lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorDim)))}
	styles.Focused = state
	styles.Blurred = state
	return styles
}
