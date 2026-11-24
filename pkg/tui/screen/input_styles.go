package screen

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// InputPrompt is the standard prompt for unlabeled terminal inputs.
const InputPrompt = "❯ "

// InputStyles paints the composer.
//
// Cursor.Color is not decoration. An unset cursor colour is not "inherit
// the theme": the renderer emits a cursor-colour reset and hands the cell
// back to the terminal, so the block you are typing into is painted in
// whatever the terminal defaults to. It only shows up once the field has
// focus, which is exactly when someone is typing - hence a composer that
// looks themed until the first keystroke and then does not.
func InputStyles(styles textarea.Styles) textarea.Styles { return CurrentPalette().InputStyles(styles) }

func (p *Palette) InputStyles(Styles textarea.Styles) textarea.Styles {
	background := p.Background
	if p.Modern {
		background = p.Color(ColorPopupBackground)
	}
	base := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Color(ColorPopupText))).Background(lipgloss.Color(background))
	state := textarea.StyleState{Base: base, Text: base, CursorLine: base, Prompt: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Color(ColorAccent))), Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Color(ColorDim))), EndOfBuffer: base}
	_, _, selected := p.PopupStyles()
	state.Selection = selected
	Styles.Focused = state
	Styles.Blurred = state
	Styles.Cursor.Color = lipgloss.Color(p.Color(ColorAccent))
	return Styles
}

// TextInputStyles applies the active palette to search fields and their cursor.
func TextInputStyles(styles textinput.Styles) textinput.Styles {
	return CurrentPalette().TextInputStyles(styles)
}

func (p *Palette) TextInputStyles(Styles textinput.Styles) textinput.Styles {
	state := textinput.StyleState{Text: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Color(ColorPopupText))), Prompt: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Color(ColorAccent))), Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Color(ColorDim))), Suggestion: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Color(ColorDim)))}
	Styles.Focused = state
	Styles.Blurred = state
	Styles.Cursor.Color = lipgloss.Color(p.Color(ColorAccent))
	return Styles
}
