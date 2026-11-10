package agent

import (
	"charm.land/lipgloss/v2"
	"sync/atomic"

	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

const (
	colorDim                = "8"
	colorSuccess            = "2"
	colorError              = "1"
	colorAccent             = "6"
	colorWarning            = "3"
	colorUser               = "4"
	colorPopupText          = "#B6C2D2"
	colorPopupBackground    = "#263744"
	colorPopupBorder        = "#65768A"
	colorSelectedText       = "#F1F4F8"
	colorSelectedBackground = "#3B526F"
	colorDisabledText       = "#77818E"
	colorDisabledNumber     = "#20262E"
	colorDisabledLabel      = "#282E36"
	colorNumberText         = "#DCE5EF"
	colorNumberBackground   = "#303944"
	colorActionBackground   = "#526F91"
	colorMode               = "#65CCD0"
	colorReady              = "#A6C59A"
	colorBusy               = "#D7BC7C"
	colorFailed             = "#E58F93"
	colorSecondaryText      = "#8895A6"
	colorPopupError         = "#EF8790"
	colorSearchMatch        = "#F3D58A"
)

// Color choices and role styles live here. Renderers derive emphasis from them.
type styles struct {
	title    lipgloss.Style
	focus    lipgloss.Style
	disabled lipgloss.Style
	dim      lipgloss.Style
	add      lipgloss.Style
	del      lipgloss.Style
	hunk     lipgloss.Style
	gate     lipgloss.Style
	status   lipgloss.Style
	user     lipgloss.Style
	think    lipgloss.Style
	err      lipgloss.Style
	warn     lipgloss.Style
}

func (m *Model) shortcutStyles() popup.ShortcutStyles {
	return popup.ShortcutStyles{
		Key:    lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorPopupText))).Bold(true),
		Action: m.styles.dim,
	}
}

func defaultStyles() styles {
	return styles{
		title:    lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorWarning))).Bold(true),
		focus:    lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorAccent))).Bold(true),
		disabled: lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorDisabledText))),
		dim:      lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorDim))),
		add:      lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorSuccess))),
		del:      lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorError))),
		hunk:     lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorAccent))),
		gate:     lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorWarning))).Bold(true),
		status:   lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorDim))),
		user:     lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorUser))).Bold(true),
		think:    lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorDim))).Italic(true),
		err:      lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorError))).Bold(true),
		warn:     lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorWarning))),
	}
}

// popupStyles is shared by menus, completion, session search and dialogs.
func popupStyles() (normal, border, selected lipgloss.Style) {
	normal = lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorPopupText))).Background(lipgloss.Color(themeColor(colorPopupBackground)))
	border = normal.Foreground(lipgloss.Color(themeColor(colorPopupBorder)))
	selected = normal.Foreground(lipgloss.Color(themeColor(colorSelectedText))).Background(lipgloss.Color(themeColor(colorSelectedBackground))).Bold(true)
	return
}

type palette struct {
	name       string
	colors     map[string]string
	background string
}

var selectedPalette atomic.Pointer[palette]
var defaultPalette = paletteFor("dark")

type themeColors struct {
	canvas, surface, text, secondary, border, accent             string
	selection, selectionText, success, warning, danger, disabled string
}

func paletteFor(name string) *palette {
	p := &palette{name: "dark", background: "#11161C", colors: map[string]string{
		colorDim: "#8E9CA9", colorSecondaryText: "#8E9CA9", colorPopupText: "#D5DEE7", colorSelectedText: "#F1F4F8", colorNumberText: "#F1F4F8",
		colorAccent: "#79C8D0", colorUser: "#79C8D0", colorMode: "#79C8D0",
		colorSuccess: "#A2C29B", colorReady: "#A2C29B", colorWarning: "#F3D58A", colorBusy: "#F3D58A", colorSearchMatch: "#F3D58A",
		colorError: "#EA9393", colorFailed: "#EA9393", colorPopupError: "#EA9393", colorPopupBorder: "#8096A8",
	}}
	if name == "light" {
		p.name = "light"
		p.background = "#F2F4F6"
		p.colors = map[string]string{
			colorDim: "#596A79", colorSecondaryText: "#596A79", colorPopupText: "#253542", colorSelectedText: "#FFFFFF", colorNumberText: "#FFFFFF",
			colorAccent: "#176B76", colorUser: "#176B76", colorMode: "#176B76", colorSuccess: "#346D43", colorReady: "#346D43",
			colorWarning: "#805600", colorBusy: "#805600", colorSearchMatch: "#805600", colorError: "#A63434", colorFailed: "#A63434", colorPopupError: "#A63434",
			colorPopupBackground: "#E0E7ED", colorPopupBorder: "#657C8E", colorSelectedBackground: "#365A77", colorActionBackground: "#365A77", colorNumberBackground: "#263D51",
			colorDisabledText: "#63717D", colorDisabledNumber: "#D1D9E0", colorDisabledLabel: "#E0E7ED",
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
		p.name, p.background = name, roles.canvas
		p.colors = map[string]string{
			colorPopupBackground: roles.surface, colorPopupText: roles.text,
			colorDim: roles.secondary, colorSecondaryText: roles.secondary, colorPopupBorder: roles.border,
			colorAccent: roles.accent, colorUser: roles.accent, colorMode: roles.accent,
			colorSelectedBackground: roles.selection, colorNumberBackground: roles.selection, colorActionBackground: roles.selection,
			colorSelectedText: roles.selectionText, colorNumberText: roles.selectionText,
			colorSuccess: roles.success, colorReady: roles.success,
			colorWarning: roles.warning, colorBusy: roles.warning, colorSearchMatch: roles.warning,
			colorError: roles.danger, colorFailed: roles.danger, colorPopupError: roles.danger,
			colorDisabledText: roles.disabled, colorDisabledLabel: roles.surface, colorDisabledNumber: roles.canvas,
		}
	}

	return p
}
func currentPalette() *palette {
	if p := selectedPalette.Load(); p != nil {
		return p
	}
	return defaultPalette
}
func setTheme(name string) { selectedPalette.Store(paletteFor(name)) }
func themeColor(token string) string {
	if c, ok := currentPalette().colors[token]; ok {
		return c
	}
	return token
}

// popupRow renders one row of a popup surface. The implementation lives in
// components/popup with the rest of the chrome; this stays so call sites in
// this package read the same as before.
func popupRow(style lipgloss.Style, text string, width int) string {
	return popup.Row(style, text, width)
}

func (m *Model) noticeStyles() pane.NoticeStyles {
	return pane.NoticeStyles{Info: m.styles.dim, Success: m.styles.add, Warning: m.styles.warn, Error: m.styles.err}
}

func (m *Model) graphStyles() graph.Styles {
	surface := popupSurface()
	background := lipgloss.Color(currentPalette().background)
	return graph.Styles{
		Normal: surface.Normal.Background(background), Title: surface.Selected,
		Focus: m.styles.focus.Background(background), Success: m.styles.add.Background(background),
		Warning: m.styles.warn.Background(background), Error: m.styles.err.Background(background),
		Disabled: m.styles.disabled.Background(background),
	}
}
