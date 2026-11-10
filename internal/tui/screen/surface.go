package screen

import (
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

// Surface supplies the current terminal geometry and theme for screen panes.
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

// Panel describes one screen pane, including its local focus and close control.
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

// SidePaneWidth is the minimum width for placing an inspector beside its canvas.
const SidePaneWidth = 100

// DefaultSurfaceStyles supplies the shared theme's pane and graph roles.
func DefaultSurfaceStyles() SurfaceStyles {
	s := DefaultStyles()
	surface := PopupSurface()
	background := lipgloss.Color(CurrentPalette().Background)
	return SurfaceStyles{
		Panels:    pane.Styles{Border: lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorPopupBorder))), Title: s.Title, Focus: s.Focus},
		Shortcuts: popup.ShortcutStyles{Key: lipgloss.NewStyle().Foreground(lipgloss.Color(Color(ColorPopupText))).Bold(true), Action: s.Dim},
		Graph:     graph.Styles{Normal: surface.Normal.Background(background), Title: surface.Selected, Focus: s.Focus.Background(background), Success: s.Add.Background(background), Warning: s.Warn.Background(background), Error: s.Err.Background(background), Disabled: s.Disabled.Background(background)},
		Notice:    pane.NoticeStyles{Info: s.Dim, Success: s.Add, Warning: s.Warn, Error: s.Err},
		Muted:     s.Dim, Accent: s.Hunk, Error: s.Err,
	}
}
