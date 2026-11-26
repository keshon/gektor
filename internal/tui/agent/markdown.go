package agent

import (
	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/pkg/tui/components/markdown"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func baseStyle(r role, st styles) lipgloss.Style {
	switch r {
	case roleUser:
		return st.User.Bold(false)
	case roleThink:
		return st.Think
	default:
		return lipgloss.NewStyle()
	}
}

func renderMdBlock(b block, raw string, st styles, width int) []string {
	base := baseStyle(b.role, st)
	appearance := screen.CurrentPalette()
	typeColor, symbolColor := "#C9ABED", "#91BAEF"
	if appearance.Name == "light" {
		typeColor, symbolColor = "#694397", "#305B90"
	}
	typeName := lipgloss.NewStyle().Foreground(lipgloss.Color(typeColor))
	symbol := lipgloss.NewStyle().Foreground(lipgloss.Color(symbolColor))
	palette := markdown.Styles{
		Text:     base,
		Muted:    st.Dim,
		Accent:   st.Hunk,
		Code:     base,
		Keyword:  st.Hunk.Bold(true),
		String:   st.Add,
		Number:   st.Warn,
		Comment:  st.Think,
		TypeName: &typeName,
		Symbol:   &symbol,
		Added:    st.Add,
		Deleted:  st.Del,
	}
	if b.role == roleThink {
		palette = markdown.Styles{
			Text: base, Muted: base, Accent: base, Code: base,
			Keyword: base, String: base, Number: base, Comment: base,
			Added: base, Deleted: base,
		}
	}
	if appearance.Modern {
		palette.Bullet = "▪"
	}
	return markdown.Render(raw, width, palette)
}
