package workflow

import (
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

// Surface supplies the current terminal geometry and theme for workflow panes.
// It is a value snapshot: rendering and hit testing need no application callbacks.
type Surface struct {
	Area                 pane.Rect
	Inset, SplitWidth    int
	Outlined, BottomEdge bool
	HeaderFocused        bool
	Styles               SurfaceStyles
}

// SurfaceStyles assigns presentation roles without consulting application state.
type SurfaceStyles struct {
	Panels               pane.Styles
	Shortcuts            popup.ShortcutStyles
	Graph                graph.Styles
	Notice               pane.NoticeStyles
	Muted, Accent, Error lipgloss.Style
}

// Panel describes one workflow pane, including its local focus and close control.
type Panel struct {
	Title, Hint, Body string
	Width             int
	Focused, Close    bool
}

// Panes places the inspector beside the canvas, or over it on narrow terminals.
func (s Surface) Panes(inspectorOpen bool) pane.SplitLayout {
	return pane.Split(s.Area, inspectorOpen, s.SplitWidth)
}

// Layout is shared by rendering and hit testing. Coordinates are pane-local.
func (s Surface) Layout(width int, close bool) pane.Layout {
	return pane.Measure(pane.Props{
		Width: width, Height: s.Area.Height, FooterHeight: 1,
		LeftInset: s.Inset, RightInset: s.Inset,
		Outlined: s.Outlined, BottomEdge: s.BottomEdge, Close: close,
	})
}

// Render keeps focus subordinate to the application menu's input ownership.
func (s Surface) Render(p Panel) []string {
	return pane.Render(s.Layout(p.Width, p.Close), p.Title, p.Body, []string{p.Hint},
		s.Styles.Panels, p.Focused && !s.HeaderFocused)
}

// Hints fits complete shortcut groups inside the inspector footer.
func (s Surface) Hints(width int, shortcuts ...popup.Shortcut) string {
	return popup.Shortcuts(s.Layout(width, true).Footer.Width, s.Styles.Shortcuts, shortcuts...)
}
