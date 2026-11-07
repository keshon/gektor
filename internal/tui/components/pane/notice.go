package pane

import (
	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

type NoticeLevel int

const (
	NoticeInfo NoticeLevel = iota
	NoticeSuccess
	NoticeWarning
	NoticeError
)

type NoticeStyles struct {
	Info, Success, Warning, Error lipgloss.Style
}

// NoticeRow keeps status text in one bounded row. Warnings and errors carry
// words as well as color so their meaning survives monochrome terminals.
func NoticeRow(text string, level NoticeLevel, width int, styles NoticeStyles) string {
	style := styles.Info
	if text != "" {
		switch level {
		case NoticeSuccess:
			style = styles.Success
		case NoticeWarning:
			text, style = "Warning: "+text, styles.Warning
		case NoticeError:
			text, style = "Error: "+text, styles.Error
		}
	}
	return style.Render(cell.Line(text, max(width, 0)))
}
