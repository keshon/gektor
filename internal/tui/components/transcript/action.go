package transcript

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

type ActionState int

const (
	ActionCompleted ActionState = iota
	ActionRunning
	ActionReturned
	ActionSkipped
	ActionFailed
)

type ActionStyles struct {
	Secondary, Success, Warning, Error lipgloss.Style
}

type ActionRow struct {
	Action, Target, Name, Failure string
	State                         ActionState
}

func actionState(state ActionState, styles ActionStyles) (glyph, label string, style lipgloss.Style) {
	switch state {
	case ActionRunning:
		return "…", "Running", styles.Warning
	case ActionReturned:
		return "·", "Returned", styles.Secondary
	case ActionSkipped:
		return "–", "Skipped for input", styles.Secondary
	case ActionFailed:
		return "×", "Failed", styles.Error
	default:
		return "✓", "Completed", styles.Success
	}
}

// ActionGlyph gives compact and detailed presentations the same state marker.
func ActionGlyph(state ActionState, styles ActionStyles) string {
	glyph, _, style := actionState(state, styles)
	return style.Render(glyph)
}

// View keeps state ahead of optional metadata and target text when space runs
// out. The caller supplies summaries, never raw output or execution behavior.
func (r ActionRow) View(width int, styles ActionStyles) string {
	width = max(width, 0)
	_, status, style := actionState(r.State, styles)
	if r.State == ActionSkipped && width < 40 {
		status = "Skipped"
	}
	if width < cell.Width(status)+6 {
		return cell.Line(ActionGlyph(r.State, styles)+" "+style.Render(status), width)
	}
	right := style.Render(status)
	if r.State == ActionFailed && strings.TrimSpace(r.Failure) != "" {
		failure := strings.Join(strings.Fields(r.Failure), " ")
		budget := min(32, max(width/3, cell.Width(status)))
		if budget >= cell.Width(status)+4 {
			right = style.Render(ansi.Truncate(status+": "+failure, budget, "…"))
		}
	}
	name := strings.Join(strings.Fields(r.Name), " ")
	if name != "" && cell.Width(name)+3+cell.Width(right) <= width/2 {
		right = styles.Secondary.Render(name+" │ ") + right
	}
	left := ActionGlyph(r.State, styles) + " " + strings.Join(strings.Fields(r.Action), " ")
	if r.Target != "" {
		left += " " + styles.Secondary.Render(strings.Join(strings.Fields(r.Target), " "))
	}
	left = ansi.Truncate(left, width-cell.Width(right)-2, "…")
	return left + strings.Repeat(" ", width-cell.Width(left)-cell.Width(right)) + right
}
