package tui

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
	workflowui "github.com/keshon/gektor/internal/tui/workflow"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
)

func (m *workflowScreen) definition() workflow.Definition {
	if m.run != nil {
		// The execution definition contains compiler bookkeeping. Orientation uses
		// the frozen authoring snapshot, never the current workspace file.
		if d, err := workflowui.Decode([]byte(m.run.RecipeSource)); err == nil {
			return d
		}
		return m.run.Definition
	}
	return m.editor.Definition
}

func (m *workflowScreen) steps() ([]workflow.Step, error) {
	d := m.definition()
	steps, err := recipe.Order(d.Steps)
	if err != nil {
		return d.Steps, err
	}
	return steps, nil
}

func (m *workflowScreen) ensureSelection() {
	if m.selectedSchedule() != nil && m.run == nil {
		return
	}
	steps, _ := m.steps()
	for _, step := range steps {
		if step.ID == m.graph.Selected {
			return
		}
	}
	m.graph.Selected = ""
	m.portSelected = false
	if len(steps) > 0 {
		m.graph.Selected = steps[0].ID
	}
	m.offset = 0
}

func workflowValue(raw json.RawMessage) string {
	var ref map[string]json.RawMessage
	if json.Unmarshal(raw, &ref) == nil {
		for _, kind := range []string{"$input", "$step"} {
			var name, pointer string
			if json.Unmarshal(ref[kind], &name) == nil && name != "" {
				_ = json.Unmarshal(ref["pointer"], &pointer)
				prefix := "Input "
				if kind == "$step" {
					prefix = "Step "
				}
				return prefix + name + pointer
			}
		}
	}
	return string(raw)
}

func (m *workflowScreen) page(key string) {
	w := m
	_, height := m.canvasSize()
	if w.inspect {
		height = max(1, height-2)
		switch key {
		case "home":
			w.offset = 0
		case "end":
			w.offset = max(0, len(m.inspectorContent())-height)
		case "pgup":
			w.offset = max(0, w.offset-height)
		case "pgdown":
			w.offset += height
		}
		return
	}
	if len(w.graph.Nodes) == 0 {
		return
	}
	switch key {
	case "home":
		w.graph.Selected = w.graph.Nodes[0].ID
	case "end":
		w.graph.Selected = w.graph.Nodes[len(w.graph.Nodes)-1].ID
	case "pgup":
		w.graph.SelectNext(-1)
	case "pgdown":
		w.graph.SelectNext(1)
	}
	width, height := m.canvasSize()
	w.graph.Reveal(width, height)
}

func (m *workflowScreen) canvasSize() (int, int) {
	view := m.presentation()
	area := view.Panes(m.inspectorOpen).Main
	layout := view.Layout(area.Width, false)
	return max(layout.Body.Width, 1), max(layout.Body.Height, 1)
}

func (m *workflowScreen) content() string {
	w := m
	view := m.presentation()
	cw, ch := m.canvasSize()
	m.ensureSelection()
	if w.run == nil {
		w.graph = m.buildGraph(w.graph)
	}
	canvas := w.graph
	if w.overview {
		canvas = w.graph.Overview(cw)
	}
	canvas.Nodes = append([]graph.Node(nil), canvas.Nodes...)
	for i := range canvas.Nodes {
		canvas.Nodes[i].Detail, canvas.Nodes[i].DetailTone = workflowCardDetail(canvas.Nodes[i].Detail)
	}
	left := canvas.View(cw, ch, view.Styles.Graph)
	if len(canvas.Nodes) == 0 {
		left = cellFrame("No workflow open.\nCtrl+O opens a recipe · H saved runs\nA adds a node · I requests an AI proposal", cw, ch)
	}
	title := w.editor.Definition.Name
	if w.editor.Dirty() {
		title += " *"
	}
	if w.run != nil {
		title = w.run.Definition.Name + " / " + w.run.Status
	}
	mode := "Workflow"
	if w.positionsLocked {
		mode += " [positions locked]"
	}
	regions := view.Panes(w.inspectorOpen)
	split := w.inspectorOpen && !regions.Covered
	leftWidth := regions.Main.Width
	footerWidth := view.Layout(leftWidth, false).Footer.Width
	rows := view.Render(workflowui.Panel{
		Title: mode + " / " + cleanText(title),
		Hint:  pane.NoticeRow(cleanText(w.notice), w.noticeLevel, footerWidth, view.Styles.Notice),
		Body:  left, Width: leftWidth, Focused: !w.inspect,
	})
	if w.inspectorOpen {
		width := regions.Side.Width
		content := m.inspectorContent()
		tabs := m.inspectorTabs()
		capacity := max(1, ch-len(tabs.Rows)-1)
		w.offset = min(w.offset, max(0, len(content)-capacity))
		visible := content[w.offset:min(len(content), w.offset+capacity)]
		inner := view.Layout(width, true).Body.Width
		body := strings.Join(tabs.Rows, "\n") + "\n\n" + strings.Join(popup.ScrollRows(visible, inner, w.offset, len(content), view.Styles.Muted), "\n")
		heading := "Inspect / " + cleanText(w.graph.Selected)
		if m.selectedSchedule() != nil && w.run == nil {
			heading = "Inspect / Schedule"
		}
		if w.rawDetails {
			heading += " / Raw"
		}
		if w.inspect {
			heading += " [focused]"
		}
		shortcuts := []popup.Shortcut{{Key: "Tab", Action: "focus inspector"}, {Key: "1–3", Action: "tabs"}}
		if w.inspect {
			shortcuts = []popup.Shortcut{{Key: "Esc", Action: "close"}, {Key: "Tab", Action: "parent"}, {Key: "1–3", Action: "tabs"}}
		}
		if w.inspect && w.run == nil && !w.busy && w.inspectorTab != 1 {
			shortcuts = []popup.Shortcut{{Key: "Esc", Action: "close"}, {Key: "Tab", Action: "parent"}, {Key: "E", Action: "edit"}, {Key: "1–3", Action: "tabs"}}
		}
		right := view.Render(workflowui.Panel{
			Title: heading, Hint: view.Hints(width, shortcuts...), Body: body,
			Width: width, Focused: w.inspect, Close: true,
		})
		if split {
			for i := range rows {
				rows[i] += right[i]
			}
		} else {
			rows = right
		}
	}
	return strings.Join(rows, "\n")
}

func (m *workflowScreen) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	w := m
	mouse := msg.Mouse()
	view := m.presentation()

	cw, ch := m.canvasSize()
	regions := view.Panes(w.inspectorOpen)
	canvasLayout := view.Layout(regions.Main.Width, false)
	sideLayout := view.Layout(regions.Side.Width, true)
	x, y := mouse.X-canvasLayout.Body.X, mouse.Y-regions.Main.Y-canvasLayout.Body.Y
	inspectorHeader := len(m.inspectorTabs().Rows) + 1
	inInspector := w.inspectorOpen && regions.Side.Contains(mouse.X, mouse.Y)
	if inInspector && sideLayout.CloseHit.Contains(mouse.X-regions.Side.X, mouse.Y-regions.Side.Y) && mouse.Button == tea.MouseLeft && isMouseClick(msg) {
		w.inspectorOpen, w.inspect = false, false
		w.thumb = scrollDrag{}
		return m, nil
	}
	if w.thumb.active {
		w.offset = popup.ScrollOffset(y-inspectorHeader, max(1, ch-inspectorHeader), len(m.inspectorContent()), w.thumb.grab)
		if _, ok := msg.(tea.MouseReleaseMsg); ok {
			w.thumb = scrollDrag{}
		}
		return m, nil
	}
	if inInspector && mouse.Button == tea.MouseLeft && isMouseClick(msg) {
		tabs := m.inspectorTabs()
		if tab := tabs.Hit(mouse.X-regions.Side.X-sideLayout.Body.X, y); tab >= 0 {
			m.setInspectorTab(tab)
			w.inspect = true
			return m, nil
		}
	}

	if _, ok := msg.(tea.MouseReleaseMsg); ok {
		dragged := w.dragging
		w.dragging = false
		w.panning = false
		if dragged {
			m.persistLayout()
		}
		return m, nil
	}
	if w.panning {
		w.graph.Pan.X -= x - w.last.X
		w.graph.Pan.Y -= y - w.last.Y
		w.last = graph.Point{X: x, Y: y}
		return m, nil
	}
	if w.dragging {
		w.graph.Move(x-w.last.X, y-w.last.Y)
		w.last = graph.Point{X: x, Y: y}
		return m, nil
	}
	if y < 0 || y >= ch {
		return m, nil
	}
	wheel := mouse.Button == tea.MouseWheelDown || mouse.Button == tea.MouseWheelUp || mouse.Button == tea.MouseWheelLeft || mouse.Button == tea.MouseWheelRight
	if wheel {
		delta := 3
		if mouse.Button == tea.MouseWheelUp || mouse.Button == tea.MouseWheelLeft {
			delta = -3
		}
		if inInspector {
			if y >= inspectorHeader {
				w.offset = max(0, w.offset+delta)
			}
		} else if mouse.Button == tea.MouseWheelLeft || mouse.Button == tea.MouseWheelRight || mouse.Mod&tea.ModShift != 0 {
			w.graph.Pan.X += delta
		} else {
			w.graph.Pan.Y += delta
		}
		return m, nil
	}
	if inInspector {
		if mouse.Button == tea.MouseLeft && isMouseClick(msg) {
			w.inspect = true
			if y >= inspectorHeader && y < ch && mouse.X == regions.Side.X+sideLayout.Body.X+sideLayout.Body.Width-1 {
				total := len(m.inspectorContent())
				top, size := popup.ScrollThumb(max(1, ch-inspectorHeader), total, w.offset)
				if y-inspectorHeader >= top && y-inspectorHeader < top+size {
					w.thumb = scrollDrag{active: true, grab: y - inspectorHeader - top}
				} else {
					w.offset = popup.ScrollOffset(y-inspectorHeader, max(1, ch-inspectorHeader), total, 0)
				}
			}
		}
		return m, nil
	}
	if x < 0 || x >= cw {
		return m, nil
	}
	if mouse.Button == tea.MouseMiddle && isMouseClick(msg) {
		w.panning = true
		w.last = graph.Point{X: x, Y: y}
		return m, nil
	}
	if mouse.Button == tea.MouseLeft && isMouseClick(msg) {
		canvas := w.graph
		if w.overview {
			canvas = w.graph.Overview(cw)
		}
		if !w.overview {
			if id, port, output := canvas.PortHit(x, y); id != "" {
				w.graph.Selected = id
				if strings.HasPrefix(id, workflowSchedulePrefix) || strings.HasPrefix(w.connectFrom, workflowSchedulePrefix) {
					w.connectFrom = ""
					w.inspectorOpen = true
					w.setNotice(pane.NoticeInfo, "Schedule starts the whole recipe; its connections are automatic")
					return m, nil
				}
				m.focusPort(port, output)
				w.inspect = false
				if w.run != nil || w.busy {
					w.inspectorOpen = true
					w.inspect = true
					return m, nil
				}
				if output {
					w.connectFrom = id
					w.connectPort = port
					w.setNotice(pane.NoticeInfo, "Choose an input port · Esc cancels")
				} else if w.connectFrom != "" {
					if (port == "@order") != (w.connectPort == "@order") {
						w.setNotice(pane.NoticeWarning, "Connect data ports together, or then → after for execution order")
						return m, nil
					}
					edit := workflowConnectionEdit{From: w.connectFrom, To: id, Pointer: w.connectPort, FixedSource: true, Draft: w.editor.Source()}
					switch {
					case port == "@order":
						edit.Field = "depends_on"
						edit.Pointer = ""
					case strings.HasPrefix(port, "args/"):
						edit.Field = "args"
						edit.Path = strings.TrimPrefix(port, "args")
					case port == "for_each" || strings.HasPrefix(port, "for_each/"):
						edit.Field = "for_each"
						edit.Path = strings.TrimPrefix(port, "for_each")
					case port == "when" || strings.HasPrefix(port, "when/"):
						edit.Field = "when"
						edit.Path = strings.TrimPrefix(port, "when")
					default:
						w.setNotice(pane.NoticeWarning, "Choose a named destination port")
						return m, nil
					}
					w.connectFrom = ""
					m.connectionEdit(edit)
				} else {
					w.setNotice(pane.NoticeWarning, "Choose an output port first · C connects by keyboard")
				}
				return m, nil
			}
		}
		id := canvas.Hit(x, y)
		w.inspect = false
		if id != "" {
			w.graph.Selected = id
			w.portSelected = false
			w.offset = 0
			w.dragging = !w.overview && !w.positionsLocked
		} else {
			w.panning = !w.overview
		}
		w.last = graph.Point{X: x, Y: y}
	}
	return m, nil
}

func workflowCardDetail(status string) (string, graph.Tone) {
	tone := graph.ToneNormal
	switch status {
	case "":
		status = "○ Not run"
	case "completed":
		status = "✓ Completed"
		tone = graph.ToneSuccess
	case "running":
		status = "▶ Running"
		tone = graph.ToneWarning
	case "failed":
		status = "× Failed"
		tone = graph.ToneError
	case "skipped":
		status = "− Skipped"
		tone = graph.ToneDisabled
	case "pending":
		status = "○ Pending"
	case "waiting_review", "Review before running":
		status = "? Awaiting approval"
		tone = graph.ToneWarning
	case "cancelled":
		status = "× Cancelled"
		tone = graph.ToneWarning
	case "interrupted":
		status = "! Interrupted"
		tone = graph.ToneWarning
	case "Disabled / condition false":
		tone = graph.ToneDisabled
	}
	return status, tone
}
