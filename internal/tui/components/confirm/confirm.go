// Package confirm owns the yes/no confirmation dialog used across the TUI.
// It replaces the ad-hoc confirmAction func() pattern with a typed model
// that renders primary/secondary buttons and returns a clear Intent.
package confirm

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Intent reports what the user chose.
type Intent int

const (
	None Intent = iota
	Primary   // confirmed — "yes" / "restart"
	Secondary // cancelled — "no" / "back"
)

// Styles holds the styling knobs the dialog uses. The parent creates one
// from its own styles package so the confirm component stays dependency-free.
type Styles struct {
	Title lipgloss.Style // bold emphasis (gate/warning)
	Dim   lipgloss.Style // muted text
}

// Model is the confirmation dialog. The parent creates one, opens it via
// Open, and reads Intent from Update.
type Model struct {
	Title          string
	Subtitle       string
	Body           []string
	PrimaryLabel   string
	SecondaryLabel string

	// internal state
	index int // 0 = primary, 1 = secondary
	focus bool
}

// New creates a ready-to-open confirmation model.
func New(title, subtitle, primaryLabel, secondaryLabel string, body ...string) Model {
	return Model{
		Title:          title,
		Subtitle:       subtitle,
		Body:           body,
		PrimaryLabel:   primaryLabel,
		SecondaryLabel: secondaryLabel,
		index:          0,
	}
}

// Open prepares the model for rendering. Call before showing.
func (m *Model) Open() {
	m.focus = true
	m.index = 0
}

// Close hides the model and resets state.
func (m *Model) Close() {
	m.focus = false
	m.index = 0
}

// Focused reports whether the dialog is currently open.
func (m *Model) Focused() bool { return m.focus }

// Update processes a key while the dialog is focused. Returns the intent.
func (m *Model) Update(msg tea.Msg) (Intent, tea.Cmd) {
	if !m.focus {
		return None, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return None, nil
	}

	switch key.String() {
	case "esc", "n", "N":
		m.Close()
		return Secondary, nil
	case "enter":
		m.Close()
		return Primary, nil
	case "y", "Y":
		m.Close()
		return Primary, nil
	}

	return None, nil
}

// View renders the confirmation dialog as a centered box with buttons.
// innerWidth is the max character width for content lines.
// The caller wraps the returned lines in renderBox to draw the border and centre it.
func (m *Model) View(innerWidth int, s Styles) []string {
	if !m.focus {
		return nil
	}

	var rows []string

	// Title (emphasis style).
	if m.Title != "" {
		rows = append(rows, cellLine(s.Title.Render(m.Title), innerWidth))
	}

	// Subtitle (dim style, may be multiline).
	if m.Subtitle != "" {
		for _, line := range strings.Split(m.Subtitle, "\n") {
			rows = append(rows, cellLine(s.Dim.Render(line), innerWidth))
		}
	}

	// Separator before body.
	if len(m.Body) > 0 || m.Title != "" || m.Subtitle != "" {
		rows = append(rows, "")
		rows = append(rows, cellLine(s.Dim.Render(strings.Repeat("─", innerWidth+2)), innerWidth))
	}

	// Body lines.
	for _, line := range m.Body {
		rows = append(rows, cellLine(line, innerWidth))
	}

	// Buttons row.
	if len(m.Body) > 0 || m.Title != "" || m.Subtitle != "" {
		rows = append(rows, "")
		rows = append(rows, cellLine(s.Dim.Render(strings.Repeat("─", innerWidth+2)), innerWidth))
	}

	primaryStyle := s.Title
	secondaryStyle := s.Dim
	if m.index == 1 {
		primaryStyle, secondaryStyle = secondaryStyle, primaryStyle
	}
	buttonRow := buttonsRow(
		m.PrimaryLabel, m.SecondaryLabel,
		innerWidth,
		primaryStyle, secondaryStyle,
	)
	rows = append(rows, buttonRow)

	// Hint.
	hint := "y " + m.PrimaryLabel + "  n/Esc " + m.SecondaryLabel
	rows = append(rows, cellLine(s.Dim.Render(hint), innerWidth))

	// Trailing blank for box spacing.
	rows = append(rows, "")

	return rows
}

// cellLine renders text into a fixed-width cell: ansi-aware truncation
// to width, zero-padded on the right.
func cellLine(s string, width int) string {
	width = max(width, 0)
	s = ansi.Truncate(strings.ReplaceAll(s, "\n", " "), width, "…")
	return s + strings.Repeat(" ", max(width-lipgloss.Width(s), 0))
}

// buttonsRow renders two buttons side by side: primary on the left,
// secondary on the right, separated by two spaces. Padded to exactly width.
func buttonsRow(primary, secondary string, width int, primaryStyle, secondaryStyle lipgloss.Style) string {
	cancelWidth := lipgloss.Width("[ " + secondary + " ]")
	if cancelWidth > width-3 {
		cancelWidth = min(width-3, max(3, cancelWidth))
	}
	budget := max(width-cancelWidth-2, 3)
	button := buttonRender(primary, budget, primaryStyle)
	cancel := buttonRender(secondary, max(width-lipgloss.Width(button)-2, 3), secondaryStyle)
	padding := max(width-lipgloss.Width(button)-lipgloss.Width(cancel)-2, 0)
	row := strings.Repeat(" ", padding) + button + "  " + cancel
	return cellLine(row, width)
}

// buttonRender renders a single pill-style button: "[ label ]".
func buttonRender(label string, budget int, style lipgloss.Style) string {
	budget = max(budget, 3)
	button := "[ " + label + " ]"
	if lipgloss.Width(button) > budget {
		if budget >= 5 {
			button = "[" + ansi.Truncate(label, budget-2, "…") + "]"
		} else {
			button = ansi.Truncate(label, budget, "…")
		}
	}
	return style.Render(button)
}
