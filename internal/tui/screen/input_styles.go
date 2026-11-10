package screen

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// InputStyles paints the composer.
//
// Cursor.Color is not decoration. An unset cursor colour is not "inherit
// the theme": the renderer emits a cursor-colour reset and hands the cell
// back to the terminal, so the block you are typing into is painted in
// whatever the terminal defaults to. It only shows up once the field has
// focus, which is exactly when someone is typing - hence a composer that
// looks themed until the first keystroke and then does not.
func InputStyles(Styles textarea.Styles) textarea.Styles {
	base := lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorPopupText))).Background(lipgloss.Color(CurrentPalette().Background))
	state := textarea.StyleState{Base: base, Text: base, CursorLine: base, Prompt: lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorAccent))), Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorDim))), EndOfBuffer: base}
	_, _, selected := PopupStyles()
	state.Selection = selected
	Styles.Focused = state
	Styles.Blurred = state
	Styles.Cursor.Color = lipgloss.Color(Color(ColorAccent))
	return Styles
}

// TextInputStyles applies the active palette to search fields and their cursor.
func TextInputStyles(Styles textinput.Styles) textinput.Styles {
	state := textinput.StyleState{Text: lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorPopupText))), Prompt: lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorAccent))), Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorDim))), Suggestion: lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorDim)))}
	Styles.Focused = state
	Styles.Blurred = state
	Styles.Cursor.Color = lipgloss.Color(Color(ColorAccent))
	return Styles
}
