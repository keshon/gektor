package workflow

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/pane"
)

func TestSurfaceRenderingMatchesInspectorHitRegions(t *testing.T) {
	for _, width := range []int{40, 80, 121} {
		for _, outlined := range []bool{false, true} {
			surface := Surface{
				Area:  pane.Rect{Y: 1, Width: width, Height: 28},
				Inset: 1, SplitWidth: 100, Outlined: outlined, BottomEdge: true,
			}
			panes := surface.Panes(true)
			if panes.Covered != (width < 100) {
				t.Fatalf("wrong inspector placement at width %d: %+v", width, panes)
			}
			layout := surface.Layout(panes.Side.Width, true)
			rows := surface.Render(Panel{Title: "Inspector", Body: "content", Hint: "hint", Width: panes.Side.Width, Close: true})
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
	surface := Surface{
		Area: pane.Rect{Width: 40, Height: 20}, Inset: 1,
		Styles: SurfaceStyles{Panels: pane.Styles{
			Title: lipgloss.NewStyle().Transform(func(s string) string { return "normal " + s }),
			Focus: lipgloss.NewStyle().Transform(func(s string) string { return "focused " + s }),
		}},
	}
	panel := Panel{Title: "Inspector", Width: 40, Focused: true}
	if !strings.Contains(strings.Join(surface.Render(panel), "\n"), "focused Inspector") {
		t.Fatal("focused pane lost its emphasis")
	}
	surface.HeaderFocused = true
	rows := strings.Join(surface.Render(panel), "\n")
	if strings.Contains(rows, "focused Inspector") || !strings.Contains(rows, "normal Inspector") {
		t.Fatal("pane retained focus while the application menu owns input")
	}
}
