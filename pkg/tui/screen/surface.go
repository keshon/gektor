package screen

import (
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/pkg/tui/components/graph"
	"github.com/keshon/gektor/pkg/tui/components/pane"
	"github.com/keshon/gektor/pkg/tui/components/popup"
)

// Surface supplies the current terminal geometry and theme for screen panes.
// It is a value snapshot: rendering and hit testing need no application callbacks.
type Surface struct {
	Palette                           *Palette
	Area                              pane.Rect
	Inset, SplitWidth, InspectorWidth int
	Outlined, BottomEdge              bool
	Quiet                             bool
	HeaderFocused                     bool
	Styles                            SurfaceStyles
}

// SurfaceStyles assigns presentation roles without consulting application state.
type SurfaceStyles struct {
	Panels               pane.Styles
	Tabs                 pane.TabStyles
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
	return pane.Split(s.Area, inspectorOpen, pane.SplitProps{MinimumWidth: s.SplitWidth, SideWidth: s.InspectorWidth})
}

// Layout is shared by rendering and hit testing. Coordinates are pane-local.
func (s Surface) Layout(width int, close bool) pane.Layout {
	return pane.Measure(pane.Props{
		Width: width, Height: s.Area.Height, FooterHeight: 1,
		LeftInset: s.Inset, RightInset: s.Inset,
		Outlined: s.Outlined, Quiet: s.Quiet && !s.Outlined, BottomEdge: s.BottomEdge, Close: close, Footerless: s.palette().Modern && close,
	})
}

// Render keeps focus subordinate to the application menu's input ownership.
func (s Surface) Render(p Panel) []string {
	rows := pane.Render(s.Layout(p.Width, p.Close), p.Title, p.Body, []string{p.Hint},
		s.Styles.Panels, p.Focused && !s.HeaderFocused)
	palette := s.palette()
	if p.Close && palette.Modern && !s.Outlined {
		for i, row := range rows {
			rows[i] = palette.PaintSurface(row, palette.Color(ColorPopupBackground))
		}
	}
	return rows
}

// Hints fits complete shortcut groups inside the inspector footer.
func (s Surface) Hints(width int, shortcuts ...popup.Shortcut) string {
	layout := s.Layout(width, true)
	if layout.Footer.Height == 0 {
		return ""
	}
	return popup.Shortcuts(layout.Footer.Width, s.Styles.Shortcuts, shortcuts...)
}

func (s Surface) palette() *Palette {
	if s.Palette != nil {
		return s.Palette
	}
	return CurrentPalette()
}

// TabStyles shares the active and focused treatment across tab controls.
func (p *Palette) TabStyles() pane.TabStyles {
	styles := p.Styles()
	tabs := pane.TabStyles{Normal: styles.Dim, Active: styles.Title, Focus: styles.Focus}
	if p.Modern {
		tabs.Filled = true
		tabs.Active = tabs.Active.Background(lipgloss.Color(p.Color(ColorSelectedBackground))).Foreground(lipgloss.Color(p.Color(ColorSelectedText)))
		tabs.Focus = tabs.Focus.Background(lipgloss.Color(p.Color(ColorAccent))).Foreground(lipgloss.Color(p.Color(ColorPopupBackground)))
	}
	return tabs
}

// SidePaneWidth is the minimum width for placing an inspector beside its canvas.
const SidePaneWidth = 100

// DefaultSurfaceStyles supplies the shared theme's pane and graph roles.
func DefaultSurfaceStyles() SurfaceStyles { return CurrentPalette().SurfaceStyles() }

func (p *Palette) SurfaceStyles() SurfaceStyles {
	s := p.Styles()
	normal, border, selected := p.PopupStyles()
	surface := popup.Styles{Normal: normal, Border: border, Selected: selected}
	background := lipgloss.Color(p.Background)
	return SurfaceStyles{
		Tabs:      p.TabStyles(),
		Panels:    pane.Styles{Border: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Color(ColorPopupBorder))), Title: s.Title, Focus: s.Focus},
		Shortcuts: popup.ShortcutStyles{Key: lipgloss.NewStyle().Foreground(lipgloss.Color(p.Color(ColorPopupText))).Bold(true), Action: s.Dim},
		Graph:     graph.Styles{Normal: surface.Normal.Background(background), Title: surface.Selected, Focus: s.Focus.Background(background), Success: s.Add.Background(background), Warning: s.Warn.Background(background), Error: s.Err.Background(background), Disabled: s.Disabled.Background(background)},
		Notice:    pane.NoticeStyles{Info: s.Dim, Success: s.Add, Warning: s.Warn, Error: s.Err},
		Muted:     s.Dim, Accent: s.Hunk, Error: s.Err,
	}
}
