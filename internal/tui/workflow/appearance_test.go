package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/pkg/tui/components/pane"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestWorkflowInspectorStyleMatrixKeepsTabsAndScrollingAligned(t *testing.T) {
	previous := screen.CurrentPalette()
	defer func() { screen.SetTheme(previous.Name); screen.SetModern(previous.Modern) }()
	for _, modern := range []bool{false, true} {
		screen.SetTheme("dark")
		screen.SetModern(modern)
		palette := screen.CurrentPalette()
		for _, outlined := range []bool{false, true} {
			for _, width := range []int{40, 80, 121} {
				parent := workflowTestModel(t)
				w := parent.workflows
				d := workflowRecipe()
				args, err := json.Marshal(map[string]any{"code": strings.Repeat("readable code line\n", 100), "input": 2})
				if err != nil {
					t.Fatal(err)
				}
				d.Steps[0].Args = args
				w.editor.Replace(d)
				height := 27
				if !modern {
					height--
				}
				surface := screen.Surface{Palette: palette, Area: pane.Rect{Y: 1, Width: width, Height: height}, Inset: 1, SplitWidth: screen.SidePaneWidth, Outlined: outlined, BottomEdge: true, Quiet: modern, Styles: palette.SurfaceStyles()}
				w.SetSurface(surface)
				w.inspectorOpen, w.inspect = true, true
				w.content()
				regions := surface.Panes(true)
				layout := surface.Layout(regions.Side.Width, true)
				for tab := 0; tab < 3; tab++ {
					hit := w.inspectorTabs().Hits[tab]
					w.mouse(tea.MouseClickMsg{X: regions.Side.X + layout.Body.X + hit.X, Y: regions.Side.Y + layout.Body.Y + hit.Y, Button: tea.MouseLeft})
					if w.inspectorTab != tab || !w.inspect {
						t.Fatal("tab target drifted", modern, outlined, width, tab)
					}
				}
				rows := strings.Split(w.content(), "\n")
				view := ansi.Strip(strings.Join(rows, "\n"))
				if len(rows) != height {
					t.Fatal("pane height changed", modern, outlined, width)
				}
				for _, row := range rows {
					if ansi.StringWidth(row) != width {
						t.Fatal("workflow pane overflow", modern, outlined, width, row)
					}
				}
				if strings.Contains(view, "[focused]") {
					t.Fatal("text focus label returned")
				}
				if modern {
					if layout.Footer.Height != 0 || strings.Contains(view, "[Code]") || strings.Contains(view, "F11 expand") {
						t.Fatal("Modern inspector retained Commander styling", outlined, width)
					}
				} else if layout.Footer.Height != 1 || !strings.Contains(view, "[Code]") || !strings.Contains(view, "ESC") {
					t.Fatal("Commander inspector lost its chrome", outlined, width)
				}
				if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" && width == 121 {
					style, frame := "commander", "borderless"
					if modern {
						style = "modern"
					}
					if outlined {
						frame = "bordered"
					}
					w.setInspectorTab(0)
					err := os.WriteFile(filepath.Join(dir, "workflow-inspector-"+style+"-"+frame+".ansi"), []byte(w.View().Content), 0600)
					if err != nil {
						t.Fatal(err)
					}
					w.setInspectorTab(2)
				}
				before := w.editor.Source()
				w.offset = 0
				w.mouse(tea.MouseWheelMsg{X: regions.Side.X + layout.Body.X + 1, Y: regions.Side.Y + layout.Body.Y + layout.Body.Height - 1, Button: tea.MouseWheelDown})
				if w.offset == 0 {
					t.Fatal("bottom inspector row does not accept scrolling", modern, outlined, width)
				}
				w.offset = 0
				x := regions.Side.X + layout.Body.X + layout.Body.Width - 1
				y := regions.Side.Y + layout.Body.Y + len(w.inspectorTabs().Rows) + 1
				w.mouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				w.mouse(tea.MouseMotionMsg{X: 0, Y: 100, Button: tea.MouseLeft})
				w.mouse(tea.MouseReleaseMsg{X: 0, Y: 100, Button: tea.MouseLeft})
				end := max(len(w.inspectorContent())-w.inspectorCapacity(), 0)
				if w.offset != end || w.editor.Source() != before {
					t.Fatal("scrolling missed the inspector end or changed the graph", modern, outlined, width, w.offset, end)
				}
				w.offset = 0
				w.page("end")
				if w.offset != end {
					t.Fatal("keyboard and mouse disagree about the inspector end", modern, outlined, width)
				}
				w.mouse(tea.MouseClickMsg{X: regions.Side.X + layout.CloseHit.X, Y: regions.Side.Y + layout.CloseHit.Y, Button: tea.MouseLeft})
				if w.inspectorOpen {
					t.Fatal("close button missed", modern, outlined, width)
				}
			}
		}
	}
}
