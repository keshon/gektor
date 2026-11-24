package screen_test

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/pkg/tui/components/pane"
	"github.com/keshon/gektor/pkg/tui/components/popup"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestSurfaceRenderingMatchesInspectorHitRegions(t *testing.T) {
	for _, width := range []int{40, 80, 121} {
		for _, outlined := range []bool{false, true} {
			surface := screen.Surface{
				Area:  pane.Rect{Y: 1, Width: width, Height: 28},
				Inset: 1, SplitWidth: 100, Outlined: outlined, BottomEdge: true,
			}
			panes := surface.Panes(true)
			if panes.Covered != (width < 100) {
				t.Fatalf("wrong inspector placement at width %d: %+v", width, panes)
			}
			layout := surface.Layout(panes.Side.Width, true)
			rows := surface.Render(screen.Panel{Title: "Inspector", Body: "content", Hint: "hint", Width: panes.Side.Width, Close: true})
			if len(rows) != surface.Area.Height {
				t.Fatalf("wrong pane height: %d", len(rows))
			}
			for _, row := range rows {
				if ansi.StringWidth(row) != panes.Side.Width {
					t.Fatalf("pane overflows: %q", row)
				}
			}
			if got := ansi.Cut(rows[layout.Body.Y], layout.Body.X, layout.Body.X+7); got != "content" {
				t.Fatalf("body is outside its hit region: %q", got)
			}
			close := ansi.Cut(rows[layout.CloseHit.Y], layout.CloseHit.X, layout.CloseHit.X+layout.CloseHit.Width)
			if !strings.Contains(close, "×") {
				t.Fatalf("close control is outside its hit region: %q", close)
			}
		}
	}
}

func TestSurfaceMenuFocusSuppressesPaneFocus(t *testing.T) {
	surface := screen.Surface{
		Area: pane.Rect{Width: 40, Height: 20}, Inset: 1,
		Styles: screen.SurfaceStyles{Panels: pane.Styles{
			Title: lipgloss.NewStyle().Transform(func(s string) string { return "normal " + s }),
			Focus: lipgloss.NewStyle().Transform(func(s string) string { return "focused " + s }),
		}},
	}
	panel := screen.Panel{Title: "Inspector", Width: 40, Focused: true}
	if !strings.Contains(strings.Join(surface.Render(panel), "\n"), "focused Inspector") {
		t.Fatal("focused pane lost its emphasis")
	}
	surface.HeaderFocused = true
	rows := strings.Join(surface.Render(panel), "\n")
	if strings.Contains(rows, "focused Inspector") || !strings.Contains(rows, "normal Inspector") {
		t.Fatal("pane retained focus while the application menu owns input")
	}
}

func TestInspectorSurfaceRetainsItsOwnStyleAndPalette(t *testing.T) {
	previous := screen.CurrentPalette()
	defer func() { screen.SetTheme(previous.Name); screen.SetModern(previous.Modern) }()
	screen.SetTheme("dark")
	screen.SetModern(true)
	modern := screen.CurrentPalette()
	screen.SetTheme("light")
	screen.SetModern(false)
	commander := screen.CurrentPalette()
	for _, palette := range []*screen.Palette{modern, commander} {
		for _, outlined := range []bool{false, true} {
			s := screen.Surface{Palette: palette, Area: pane.Rect{Width: 60, Height: 28}, Inset: 1, BottomEdge: true, Outlined: outlined, Quiet: palette.Modern, Styles: palette.SurfaceStyles()}
			layout := s.Layout(60, true)
			rows := s.Render(screen.Panel{Title: "Inspect", Body: "Content", Hint: "COMMANDER_FOOTER", Width: 60, Close: true})
			rendered := strings.Join(rows, "\n")
			if palette.Modern {
				if layout.Footer.Height != 0 || strings.Contains(rendered, "COMMANDER_FOOTER") || s.Hints(60, popup.Shortcut{Key: "Esc", Action: "close"}) != "" {
					t.Fatal("Modern inspector kept the footer", outlined)
				}
				if !outlined {
					prefix := strings.TrimSuffix(palette.PaintSurface("", palette.Color(screen.ColorPopupBackground)), "\x1b[m")
					for _, row := range rows {
						if !strings.HasPrefix(row, prefix) || !strings.HasSuffix(row, "\x1b[m") {
							t.Fatal("inspector row used the global palette or leaked its background")
						}
					}
				}
			} else if layout.Footer.Height != 1 || !strings.Contains(rendered, "COMMANDER_FOOTER") {
				t.Fatal("Commander lost its footer", outlined)
			}
			if s.Styles.Tabs.Filled != palette.Modern {
				t.Fatal("tabs do not match the surface style")
			}
		}
	}
}
