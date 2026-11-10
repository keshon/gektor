package screen

import (
	"sync/atomic"

	"charm.land/lipgloss/v2"
)

// Color roles retain stable tokens across palette changes. Resolve them with Color.
const (
	ColorDim                = "8"
	ColorSuccess            = "2"
	ColorError              = "1"
	ColorAccent             = "6"
	ColorWarning            = "3"
	ColorUser               = "4"
	ColorPopupText          = "#B6C2D2"
	ColorPopupBackground    = "#263744"
	ColorPopupBorder        = "#65768A"
	ColorSelectedText       = "#F1F4F8"
	ColorSelectedBackground = "#3B526F"
	ColorDisabledText       = "#77818E"
	ColorDisabledNumber     = "#20262E"
	ColorDisabledLabel      = "#282E36"
	ColorNumberText         = "#DCE5EF"
	ColorNumberBackground   = "#303944"
	ColorActionBackground   = "#526F91"
	ColorMode               = "#65CCD0"
	ColorReady              = "#A6C59A"
	ColorBusy               = "#D7BC7C"
	ColorFailed             = "#E58F93"
	ColorSecondaryText      = "#8895A6"
	ColorPopupError         = "#EF8790"
	ColorSearchMatch        = "#F3D58A"
)

// Styles groups semantic text roles shared by screens and overlays.
type Styles struct {
	Title    lipgloss.Style
	Focus    lipgloss.Style
	Disabled lipgloss.Style
	Dim      lipgloss.Style
	Add      lipgloss.Style
	Del      lipgloss.Style
	Hunk     lipgloss.Style
	Gate     lipgloss.Style
	Status   lipgloss.Style
	User     lipgloss.Style
	Think    lipgloss.Style
	Err      lipgloss.Style
	Warn     lipgloss.Style
}

// DefaultStyles builds semantic text styles for the currently selected palette.
func DefaultStyles() Styles {
	return Styles{
		Title:    lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorWarning))).Bold(true),
		Focus:    lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorAccent))).Bold(true),
		Disabled: lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorDisabledText))),
		Dim:      lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorDim))),
		Add:      lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorSuccess))),
		Del:      lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorError))),
		Hunk:     lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorAccent))),
		Gate:     lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorWarning))).Bold(true),
		Status:   lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorDim))),
		User:     lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorUser))).Bold(true),
		Think:    lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorDim))).Italic(true),
		Err:      lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorError))).Bold(true),
		Warn:     lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorWarning))),
	}
}

// PopupStyles returns normal, border, and selected styles for shared overlay surfaces.
func PopupStyles() (normal, border, selected lipgloss.Style) {
	normal = lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorPopupText))).Background(lipgloss.Color(Color(ColorPopupBackground)))
	border = normal.Foreground(lipgloss.Color(Color(ColorPopupBorder)))
	selected = normal.Foreground(lipgloss.Color(Color(ColorSelectedText))).Background(lipgloss.Color(Color(ColorSelectedBackground))).Bold(true)
	return
}

// Palette is an immutable theme snapshot shared by both screens.
type Palette struct {
	Name       string
	Background string
	colors     map[string]string
}

var selectedPalette atomic.Pointer[Palette]
var defaultPalette = paletteFor("dark")

type themeColors struct {
	canvas, surface, text, secondary, border, accent             string
	selection, selectionText, success, warning, danger, disabled string
}

func paletteFor(name string) *Palette {
	p := &Palette{Name: "dark", Background: "#11161C", colors: map[string]string{
		ColorDim: "#8E9CA9", ColorSecondaryText: "#8E9CA9", ColorPopupText: "#D5DEE7", ColorSelectedText: "#F1F4F8", ColorNumberText: "#F1F4F8",
		ColorAccent: "#79C8D0", ColorUser: "#79C8D0", ColorMode: "#79C8D0",
		ColorSuccess: "#A2C29B", ColorReady: "#A2C29B", ColorWarning: "#F3D58A", ColorBusy: "#F3D58A", ColorSearchMatch: "#F3D58A",
		ColorError: "#EA9393", ColorFailed: "#EA9393", ColorPopupError: "#EA9393", ColorPopupBorder: "#8096A8",
	}}
	if name == "light" {
		p.Name = "light"
		p.Background = "#F2F4F6"
		p.colors = map[string]string{
			ColorDim: "#596A79", ColorSecondaryText: "#596A79", ColorPopupText: "#253542", ColorSelectedText: "#FFFFFF", ColorNumberText: "#FFFFFF",
			ColorAccent: "#176B76", ColorUser: "#176B76", ColorMode: "#176B76", ColorSuccess: "#346D43", ColorReady: "#346D43",
			ColorWarning: "#805600", ColorBusy: "#805600", ColorSearchMatch: "#805600", ColorError: "#A63434", ColorFailed: "#A63434", ColorPopupError: "#A63434",
			ColorPopupBackground: "#E0E7ED", ColorPopupBorder: "#657C8E", ColorSelectedBackground: "#365A77", ColorActionBackground: "#365A77", ColorNumberBackground: "#263D51",
			ColorDisabledText: "#63717D", ColorDisabledNumber: "#D1D9E0", ColorDisabledLabel: "#E0E7ED",
		}
	}
	// Each theme maps every surface role; no control falls back to another
	// theme's background or disabled colors.
	var roles *themeColors
	switch name {
	case "one-half-dark":
		roles = &themeColors{"#282C34", "#303640", "#DCDFE4", "#A2AAB6", "#788594", "#61AFEF", "#3E566E", "#FFFFFF", "#98C379", "#E5C07B", "#E98890", "#7E8795"}
	case "one-half-light":
		roles = &themeColors{"#FAFAFA", "#ECEFF2", "#30343B", "#5C6572", "#8492A1", "#196AA5", "#D6E6F4", "#1C3851", "#3B7342", "#80580B", "#AC353D", "#737D89"}
	case "solarized-dark":
		roles = &themeColors{"#002B36", "#073642", "#D0DDDC", "#9EAFB0", "#688889", "#59C7BB", "#134F5C", "#F3F5E8", "#A7BF70", "#E7B65C", "#F28B81", "#7F999D"}
	case "solarized-light":
		roles = &themeColors{"#FDF6E3", "#EEE8D5", "#43565B", "#5E7378", "#80958B", "#116D75", "#D5E4D4", "#274448", "#46712E", "#855500", "#AB3D35", "#73817C"}
	}
	if roles != nil {
		p.Name, p.Background = name, roles.canvas
		p.colors = map[string]string{
			ColorPopupBackground: roles.surface, ColorPopupText: roles.text,
			ColorDim: roles.secondary, ColorSecondaryText: roles.secondary, ColorPopupBorder: roles.border,
			ColorAccent: roles.accent, ColorUser: roles.accent, ColorMode: roles.accent,
			ColorSelectedBackground: roles.selection, ColorNumberBackground: roles.selection, ColorActionBackground: roles.selection,
			ColorSelectedText: roles.selectionText, ColorNumberText: roles.selectionText,
			ColorSuccess: roles.success, ColorReady: roles.success,
			ColorWarning: roles.warning, ColorBusy: roles.warning, ColorSearchMatch: roles.warning,
			ColorError: roles.danger, ColorFailed: roles.danger, ColorPopupError: roles.danger,
			ColorDisabledText: roles.disabled, ColorDisabledLabel: roles.surface, ColorDisabledNumber: roles.canvas,
		}
	}

	return p
}

// CurrentPalette returns the shared immutable palette; callers must not modify it.
func CurrentPalette() *Palette {
	if p := selectedPalette.Load(); p != nil {
		return p
	}
	return defaultPalette
}

// SetTheme atomically replaces the shared palette, falling back to dark for unknown names.
func SetTheme(name string) { selectedPalette.Store(paletteFor(name)) }

// Color resolves a semantic role and returns unrecognized color tokens unchanged.
func Color(token string) string {
	if c, ok := CurrentPalette().colors[token]; ok {
		return c
	}
	return token
}
