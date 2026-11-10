package agent

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/components/graph"
	workflowui "github.com/keshon/gektor/internal/tui/workflow"
)

func TestWorkflowCardTitleAndSelectionInAllThemes(t *testing.T) {
	previous := selectedPalette.Load()
	defer selectedPalette.Store(previous)
	for _, theme := range config.ThemeNames() {
		m := workflowTestModel(t)
		m.applyTheme(theme)
		d := workflowRecipe()
		next := d.Steps[0]
		next.ID = "other"
		d.Steps = append(d.Steps, next)
		draft := seedWorkflow(t, m, d)
		view := m.View().Content
		screen := uv.NewScreenBuffer(m.termW, m.termH)
		uv.NewStyledString(view).Draw(screen, uv.Rect(0, 0, m.termW, m.termH))
		rows := strings.Split(ansi.Strip(view), "\n")
		for _, n := range draft.Graph(graph.Model{}).Nodes {
			title := screen.CellAt(n.X+3, paneTop+n.Y+1)
			if title == nil || !sameColor(title.Style.Fg, themeColor(colorSelectedText)) || !sameColor(title.Style.Bg, themeColor(colorSelectedBackground)) {
				t.Fatalf("%s: unreadable title for %s: %+v", theme, n.ID, title)
			}
			border := screen.CellAt(n.X+1, paneTop+n.Y)
			borderColor := colorPopupText
			if n.ID == "transform" {
				borderColor = colorAccent
			}
			if border == nil || !sameColor(border.Style.Fg, themeColor(borderColor)) {
				t.Fatalf("%s: frame uses title foreground on canvas", theme)
			}
			corner := "┌"
			if n.ID == "transform" {
				corner = "╔"
			}
			if ansi.Cut(rows[paneTop+n.Y], n.X+1, n.X+2) != corner {
				t.Fatalf("%s: selected frame not distinct", theme)
			}
		}
	}
}

func TestWorkflowNodeStateColorsDoNotReplaceSelection(t *testing.T) {
	defer setTheme("dark")
	m := workflowTestModel(t)
	for _, theme := range config.ThemeNames() {
		m.applyTheme(theme)
		for _, item := range []struct {
			status, label, color string
		}{
			{"completed", "Completed", colorSuccess},
			{"running", "Running", colorWarning},
			{"failed", "Failed", colorError},
			{"waiting_review", "Awaiting approval", colorWarning},
			{"cancelled", "Cancelled", colorWarning},
			{"interrupted", "Interrupted", colorWarning},
			{"skipped", "Skipped", colorDisabledText},
			{"Disabled / condition false", "Disabled", colorDisabledText},
			{"pending", "Pending", colorPopupText},
		} {
			for _, selected := range []string{"", "step"} {
				n := graph.Node{ID: "step"}
				n.Detail, n.DetailTone = workflowui.CardDetail(item.status)
				g := graph.Model{Nodes: []graph.Node{n}, Selected: selected}
				view := g.View(graph.NodeWidth, n.Height(), m.graphStyles())
				screen := uv.NewScreenBuffer(graph.NodeWidth, n.Height())
				uv.NewStyledString(view).Draw(screen, uv.Rect(0, 0, graph.NodeWidth, n.Height()))
				status := screen.CellAt(2, n.Height()-2)
				if status == nil || !sameColor(status.Style.Fg, themeColor(item.color)) || !sameColor(status.Style.Bg, currentPalette().background) {
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
