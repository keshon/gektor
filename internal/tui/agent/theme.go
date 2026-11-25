package agent

import "github.com/keshon/gektor/pkg/tui/screen"

// RefreshTheme rebuilds style-dependent caches after the application changes its palette.
func (m *Model) RefreshTheme() {
	modern := currentPalette().Modern
	m.input.CompactPastes = modern
	m.input.Prompt = screen.InputPrompt
	if modern {
		m.input.Prompt = "▎ "
	} else {
		m.input.Unfold()
	}
	m.styles = defaultStyles()
	m.input.SetStyles(inputStyles(m.input.Styles()))
	m.note.SetStyles(textInputStyles(m.note.Styles()))
	if m.sessions != nil {
		m.sessions.filter.SetStyles(textInputStyles(m.sessions.filter.Styles()))
		if m.sessions.mode == sessRename {
			m.sessions.rename.SetStyles(textInputStyles(m.sessions.rename.Styles()))
		}
	}
	m.transcript.render.cache = nil
	m.refreshContent()
	m.inspector.pane.render.cache = nil
	m.refreshInspector()
}
