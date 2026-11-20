package fields

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/pkg/tui/components/cell"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/components/popup"
)

func (f *Model) HasFeedback() bool {
	return f.Error != "" || len(f.FieldErrors) > 0
}

func (f *Model) feedbackLines(width int) []string {
	lines := cell.Wrap(f.Error, width)
	for _, field := range f.Fields {
		message := f.FieldErrors[field.Key]
		if message == "" {
			continue
		}
		label := field.Label
		if custom := f.FieldLabels[field.Key]; custom != "" {
			label = custom
		}
		lines = append(lines, cell.Wrap(label+": "+message, width)...)
	}
	return lines
}

func (f *Model) feedbackRows(props Props) ([]string, []form.Hit, int, int) {
	l := f.layoutFeedback(props)
	return l.Rows, l.Hits, props.Width, props.Height
}

func (f *Model) layoutFeedback(props Props) form.Layout {
	inner := max(popup.ModalWidth(props.Width, 0)-4, 1)
	rows := popup.Header("Error details", inner, props.Styles.Title)
	lines := f.feedbackLines(max(inner-2, 1))
	capacity := max(props.Height-len(rows)-6, 1)
	offset := min(max(f.FeedbackOffset, 0), max(len(lines)-capacity, 0))
	body := lines[offset:min(offset+capacity, len(lines))]
	rows = append(rows, popup.ScrollRows(body, inner, offset, len(lines), props.Surface.Normal)...)
	position := fmt.Sprintf("%d–%d / %d", offset+1, min(offset+capacity, len(lines)), len(lines))
	rows = append(rows, props.Styles.Muted.Render(cell.Line(position, inner)))
	hint := popup.Shortcuts(inner, popup.ShortcutStyles{Key: props.Surface.Normal.Bold(true), Action: props.Styles.Muted},
		popup.Shortcut{Key: "Esc/Enter", Action: "back"}, popup.Shortcut{Key: "↑↓", Action: "scroll"})
	rows = append(rows, popup.Footer(inner, hint, lipgloss.NewStyle())...)
	hits := []form.Hit{{Index: -1, Y0: 0, Y1: 1, X0: max(inner-3, 0), X1: inner}}
	track := popup.ScrollTrack{X: inner - 1, Y: 2, Height: len(body), Total: len(lines), Offset: offset}
	return form.Layout{Rows: rows, Hits: hits, Scroll: track}
}

func (f *Model) feedbackKey(msg tea.KeyPressMsg, props Props) {
	inner := max(popup.ModalWidth(props.Width, 0)-6, 1)
	capacity := max(props.Height-8, 1)
	limit := max(len(f.feedbackLines(inner))-capacity, 0)
	switch msg.String() {
	case "esc", "enter", "ctrl+e":
		f.FeedbackOpen = false
		f.SyncInputFocus(true)
	case "up":
		f.FeedbackOffset--
	case "down":
		f.FeedbackOffset++
	case "pgup":
		f.FeedbackOffset -= capacity
	case "pgdown":
		f.FeedbackOffset += capacity
	case "home":
		f.FeedbackOffset = 0
	case "end":
		f.FeedbackOffset = limit
	}
	f.FeedbackOffset = min(max(f.FeedbackOffset, 0), limit)
}

func (f *Model) feedbackMouse(msg tea.MouseMsg, props Props) {
	x, y := f.Origin(props)
	box := f.box(props)
	width := cell.Width(strings.Join(box, "\n"))
	mouse := msg.Mouse()
	if mouse.X < x-2 || mouse.X >= x-2+width || mouse.Y < y-1 || mouse.Y >= y-1+len(box) {
		return
	}
	switch mouse.Button {
	case tea.MouseWheelUp:
		f.feedbackKey(tea.KeyPressMsg{Code: tea.KeyUp}, props)
	case tea.MouseWheelDown:
		f.feedbackKey(tea.KeyPressMsg{Code: tea.KeyDown}, props)
	case tea.MouseLeft:
		_, hits, _, _ := f.feedbackRows(props)
		if isClick(msg) && mouse.Y == y && mouse.X >= x+hits[0].X0 && mouse.X < x+hits[0].X1 {
			f.feedbackKey(tea.KeyPressMsg{Code: tea.KeyEscape}, props)
		}
	}
}
