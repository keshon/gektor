package modal

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Model is a structured modal: head, title, subtitle, body, action, and
// help tips, separated by horizontal rules. Every field is optional;
// empty fields collapse so simple dialogs stay compact.
type Model struct {
	Head     string
	Title    string
	Subtitle string
	Body     []string
	Action   string
	Help     string
	Width    int
}

// Render produces the modal as a single string, ready for renderBox or
// direct display. Separators are dim rules across the inner width.
func (m Model) Render(styles ModalStyles) string {
	inner := m.Width
	if inner < 10 {
		inner = 10
	}
	var rows []string
	if m.Head != "" {
		rows = append(rows, styles.Head.Render(m.Head))
	}
	if m.Title != "" {
		rows = append(rows, styles.Title.Render(m.Title))
	}
	if m.Subtitle != "" {
		for _, line := range strings.Split(m.Subtitle, "\n") {
			rows = append(rows, styles.Subtitle.Render(line))
		}
	}
	if len(rows) > 0 {
		rows = append(rows, styles.Separator.Render(strings.Repeat("─", inner)))
	}
	for _, line := range m.Body {
		rows = append(rows, line)
	}
	if len(m.Body) > 0 && (m.Action != "" || m.Help != "") {
		rows = append(rows, styles.Separator.Render(strings.Repeat("─", inner)))
	}
	if m.Action != "" {
		rows = append(rows, styles.Action.Render(m.Action))
	}
	if m.Help != "" {
		rows = append(rows, styles.Help.Render(m.Help))
	}
	return strings.Join(rows, "\n")
}

type ModalStyles struct {
	Head       lipgloss.Style
	Title      lipgloss.Style
	Subtitle   lipgloss.Style
	Separator  lipgloss.Style
	Action     lipgloss.Style
	Help       lipgloss.Style
}
