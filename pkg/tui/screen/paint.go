package screen

import (
	"bytes"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// Paint sets every cell color explicitly while preserving semantic colors within the content.
func Paint(content string) string { return CurrentPalette().paint(content, colorprofile.Unknown) }

func (p *Palette) paint(content string, profile colorprofile.Profile) string {
	sequence := func(code int, color string) string {
		r, g, b, _ := lipgloss.Color(color).RGBA()
		return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", code, r>>8, g>>8, b>>8)
	}
	fg := sequence(38, p.Color(ColorPopupText))
	bg := sequence(48, p.Background)
	reset := fg + bg
	restore := strings.NewReplacer(
		"\x1b[m", "\x1b[m"+reset,
		"\x1b[0m", "\x1b[0m"+reset,
		"\x1b[39m", fg,
		"\x1b[49m", bg,
	)
	painted := reset + restore.Replace(content) + "\x1b[m"
	if profile == colorprofile.Unknown || profile == colorprofile.TrueColor {
		return painted
	}

	var output bytes.Buffer
	writer := colorprofile.Writer{Forward: &output, Profile: profile}
	// bytes.Buffer accepts every write without an error.
	_, _ = writer.WriteString(painted)
	return output.String()
}
