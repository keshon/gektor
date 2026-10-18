package tui

import (
	"charm.land/lipgloss/v2"
	"sync/atomic"

	"github.com/keshon/tars/internal/tui/components/popup"
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
	dim    lipgloss.Style
	add    lipgloss.Style
	del    lipgloss.Style
	hunk   lipgloss.Style
	gate   lipgloss.Style
	status lipgloss.Style
	user   lipgloss.Style
	think  lipgloss.Style
	err    lipgloss.Style
	warn   lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		dim:    lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorDim))),
		add:    lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorSuccess))),
		del:    lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorError))),
		hunk:   lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorAccent))),
		gate:   lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorWarning))).Bold(true),
		status: lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorDim))),
		user:   lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorUser))).Bold(true),
		think:  lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorDim))).Italic(true),
		err:    lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorError))).Bold(true),
		warn:   lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorWarning))),
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
