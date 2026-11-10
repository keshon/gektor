package agent

import (
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/screen"
)

const (
	colorDim                = screen.ColorDim
	colorSuccess            = screen.ColorSuccess
	colorError              = screen.ColorError
	colorAccent             = screen.ColorAccent
	colorWarning            = screen.ColorWarning
	colorUser               = screen.ColorUser
	colorPopupText          = screen.ColorPopupText
	colorPopupBackground    = screen.ColorPopupBackground
	colorPopupBorder        = screen.ColorPopupBorder
	colorSelectedText       = screen.ColorSelectedText
	colorSelectedBackground = screen.ColorSelectedBackground
	colorDisabledText       = screen.ColorDisabledText
	colorDisabledNumber     = screen.ColorDisabledNumber
	colorDisabledLabel      = screen.ColorDisabledLabel
	colorNumberText         = screen.ColorNumberText
	colorNumberBackground   = screen.ColorNumberBackground
	colorActionBackground   = screen.ColorActionBackground
	colorMode               = screen.ColorMode
	colorReady              = screen.ColorReady
	colorBusy               = screen.ColorBusy
	colorFailed             = screen.ColorFailed
	colorSecondaryText      = screen.ColorSecondaryText
	colorPopupError         = screen.ColorPopupError
	colorSearchMatch        = screen.ColorSearchMatch
)

type styles = screen.Styles

func defaultStyles() styles                                         { return screen.DefaultStyles() }
func currentPalette() *screen.Palette                               { return screen.CurrentPalette() }
func setTheme(name string)                                          { screen.SetTheme(name) }
func themeColor(token string) string                                { return screen.Color(token) }
func popupStyles() (lipgloss.Style, lipgloss.Style, lipgloss.Style) { return screen.PopupStyles() }
func (m *Model) shortcutStyles() popup.ShortcutStyles {
	return popup.ShortcutStyles{
		Key:    lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorPopupText))).Bold(true),
		Action: m.styles.Dim,
	}
}

// popupRow renders one row of a popup surface. The implementation lives in
// components/popup with the rest of the chrome; this stays so call sites in
// this package read the same as before.
func popupRow(style lipgloss.Style, text string, width int) string {
	return popup.Row(style, text, width)
}

func (m *Model) noticeStyles() pane.NoticeStyles {
	return pane.NoticeStyles{Info: m.styles.Dim, Success: m.styles.Add, Warning: m.styles.Warn, Error: m.styles.Err}
}

func (m *Model) graphStyles() graph.Styles {
	surface := popupSurface()
	background := lipgloss.Color(currentPalette().Background)
	return graph.Styles{
		Normal: surface.Normal.Background(background), Title: surface.Selected,
		Focus: m.styles.Focus.Background(background), Success: m.styles.Add.Background(background),
		Warning: m.styles.Warn.Background(background), Error: m.styles.Err.Background(background),
		Disabled: m.styles.Disabled.Background(background),
	}
}
