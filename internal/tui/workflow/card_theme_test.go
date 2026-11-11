package workflow

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/screen"
)

func TestWorkflowNodeStateColorsDoNotReplaceSelection(t *testing.T) {
	defer screen.SetTheme("dark")
	for _, theme := range config.ThemeNames() {
		screen.SetTheme(theme)
		for _, item := range []struct {
			status, label, color string
		}{
			{"completed", "Completed", screen.ColorSuccess},
			{"running", "Running", screen.ColorWarning},
			{"failed", "Failed", screen.ColorError},
			{"waiting_review", "Awaiting approval", screen.ColorWarning},
			{"cancelled", "Cancelled", screen.ColorWarning},
			{"interrupted", "Interrupted", screen.ColorWarning},
			{"skipped", "Skipped", screen.ColorDisabledText},
			{"Disabled / condition false", "Disabled", screen.ColorDisabledText},
			{"pending", "Pending", screen.ColorPopupText},
		} {
			for _, selected := range []string{"", "step"} {
				n := graph.Node{ID: "step"}
				n.Detail, n.DetailTone = CardDetail(item.status)
				g := graph.Model{Nodes: []graph.Node{n}, Selected: selected}
				view := g.View(graph.NodeWidth, n.Height(), screen.DefaultSurfaceStyles().Graph)
				buffer := uv.NewScreenBuffer(graph.NodeWidth, n.Height())
				uv.NewStyledString(view).Draw(buffer, uv.Rect(0, 0, graph.NodeWidth, n.Height()))
				status := buffer.CellAt(2, n.Height()-2)
				if status == nil || !sameCardColor(status.Style.Fg, screen.Color(item.color)) || !sameCardColor(status.Style.Bg, screen.CurrentPalette().Background) {
					t.Fatalf("%s %s selected=%s: wrong status colors", theme, item.status, selected)
				}
				corner := "┌"
				if selected != "" {
					corner = "╔"
				}
				if !strings.HasPrefix(ansi.Strip(view), corner) || !strings.Contains(ansi.Strip(view), item.label) {
					t.Fatal("execution state replaced selection or lost its text", view)
				}
			}
		}
	}
}

func sameCardColor(c color.Color, want string) bool {
	if c == nil {
		return false
	}
	r, g, b, _ := c.RGBA()
	return strings.EqualFold(fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8), want)
}
