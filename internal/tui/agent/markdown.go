package agent

import (
	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/pkg/tui/components/markdown"
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
	palette := markdown.Styles{
		Text:    base,
		Muted:   st.Dim,
		Accent:  st.Hunk,
		Code:    base,
		Keyword: st.Hunk.Bold(true),
		String:  st.Add,
		Number:  st.Warn,
		Comment: st.Think,
		Added:   st.Add,
		Deleted: st.Del,
	}
	if b.role == roleThink {
		palette = markdown.Styles{
			Text: base, Muted: base, Accent: base, Code: base,
			Keyword: base, String: base, Number: base, Comment: base,
			Added: base, Deleted: base,
		}
	}
	return markdown.Render(raw, width, palette)
}
