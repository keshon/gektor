package screen

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/settings"
)

// Open replaces the overlay and notifies its focus owner before showing it.
func (m *Controls) Open(title string, lines []string) {
	if m.Opening != nil {
		m.Opening(m.Dialog == nil)
	}
	m.Dialog = &DialogState{Title: title, Lines: lines}
	if m.Changed != nil {
		m.Changed()
	}
}

// Close runs the overlay cleanup and restores the underlying focus owner.
func (m *Controls) Close() {
	if m.Dialog != nil && m.Dialog.OnClose != nil {
		m.Dialog.OnClose()
	}
	m.Dialog = nil
	if m.Closed != nil {
		m.Closed()
	}
	if m.Changed != nil {
		m.Changed()
	}
}

// Area returns the absolute terminal rectangle available to overlays.
func (m *Controls) Area() (int, int, int, int) { a := m.Props.Area; return a.X, a.Y, a.Width, a.Height }

// ShortcutStyles uses the active palette for shortcut keys and descriptions.
func (m *Controls) ShortcutStyles() popup.ShortcutStyles {
	return popup.ShortcutStyles{Key: lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorPopupText))).Bold(true), Action: m.Props.Styles.Dim}
}

// PopupSurface returns the current palette roles for overlay surfaces.
func PopupSurface() popup.Styles {
	normal, border, selected := PopupStyles()
	return popup.Styles{Normal: normal, Border: border, Selected: selected}
}

// RenderBox centers a bordered overlay within the supplied cell bounds.
func RenderBox(rows []string, width, height int) string {
	return popup.FrameBox(rows, width, height, PopupSurface())
}
func cleanText(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// NewEditor creates a multiline editor with the shared text and cursor styles.
func (m *Controls) NewEditor() textarea.Model {
	editor := textarea.New()
	editor.Prompt = "> "
	editor.SetVirtualCursor(false)
	style := InputStyles(editor.Styles())
	style.Cursor.Shape = tea.CursorUnderline
	editor.SetStyles(style)
	editor.MaxHeight = 6
	editor.DynamicHeight = true
	editor.MaxContentHeight = 4096
	editor.ShowLineNumbers = false
	editor.KeyMap.InsertNewline.SetKeys("shift+enter", "ctrl+o")
	return editor
}

// SettingsProps supplies the current dialog bounds and styles to its form.
func (m *Controls) SettingsProps() settings.Props {
	a := m.Props.Area
	s := m.Props.Styles
	return settings.Props{Width: a.Width, Height: a.Height, Left: a.X, Top: a.Y, Title: m.Dialog.Title,
		Styles:  settings.Styles{Title: s.Title, Disabled: s.Disabled, Muted: s.Dim, Focus: s.Focus, Error: s.Err, Selected: s.Hunk},
		Surface: PopupSurface(), SearchStyles: TextInputStyles(textinput.New().Styles())}
}

// IsMouseClick distinguishes a press from motion, wheel, and release messages.
func IsMouseClick(msg tea.MouseMsg) bool { _, ok := msg.(tea.MouseClickMsg); return ok }

// HelpRows stacks descriptions below keys when two columns would not fit.
func (m *Controls) HelpRows(section HelpSection, width int) []string {
	keyWidth := 0
	for _, row := range section.Rows {
		keyWidth = max(keyWidth, lipgloss.Width(row[0]))
	}
	var rows []string
	for _, row := range section.Rows {
		if width-keyWidth-3 < 24 {
			rows = append(rows, m.Props.Styles.Gate.Render(row[0]))
			for _, line := range strings.Split(lipgloss.NewStyle().Width(max(width-2, 1)).Render(row[1]), "\n") {
				rows = append(rows, "  "+line)
			}
		} else {
			description := strings.Split(lipgloss.NewStyle().Width(width-keyWidth-3).Render(row[1]), "\n")
			rows = append(rows, m.Props.Styles.Gate.Render(cell.Line(row[0], keyWidth))+"   "+description[0])
			for _, line := range description[1:] {
				rows = append(rows, strings.Repeat(" ", keyWidth+3)+line)
			}
		}
		rows = append(rows, "")
	}
	return rows[:max(len(rows)-1, 0)]
}
