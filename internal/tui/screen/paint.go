package screen

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Paint sets every cell color explicitly while preserving semantic colors within the content.
func Paint(content string) string {
	sequence := func(code int, color string) string {
		r, g, b, _ := lipgloss.Color(color).RGBA()
		return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", code, r>>8, g>>8, b>>8)
	}
	fg := sequence(38, Color(ColorPopupText))
	bg := sequence(48, CurrentPalette().Background)
	reset := fg + bg
	restore := strings.NewReplacer(
		"\x1b[m", "\x1b[m"+reset,
		"\x1b[0m", "\x1b[0m"+reset,
		"\x1b[39m", fg,
		"\x1b[49m", bg,
	)
	return reset + restore.Replace(content) + "\x1b[m"
}
