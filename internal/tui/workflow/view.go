package workflow

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
)

func (m *Model) definition() workflow.Definition {
	if m.run != nil {
		// The execution definition contains compiler bookkeeping. Orientation uses
		// the frozen authoring snapshot, never the current workspace file.
		if d, err := Decode([]byte(m.run.RecipeSource)); err == nil {
			return d
		}
		return m.run.Definition
	}
	return m.editor.Definition
}

func (m *Model) steps() ([]workflow.Step, error) {
	d := m.definition()
	steps, err := recipe.Order(d.Steps)
	if err != nil {
		return d.Steps, err
	}
	return steps, nil
}

func (m *Model) ensureSelection() {
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

func (m *Model) page(key string) {
	_, height := m.canvasSize()
	if m.inspect {
		height = max(1, height-2)
		switch key {
		case "home":
			m.offset = 0
		case "end":
			m.offset = max(0, len(m.inspectorContent())-height)
		case "pgup":
			m.offset = max(0, m.offset-height)
		case "pgdown":
			m.offset += height
		}
		return
	}
	if len(m.graph.Nodes) == 0 {
		return
	}
	switch key {
	case "home":
		m.graph.Selected = m.graph.Nodes[0].ID
	case "end":
		m.graph.Selected = m.graph.Nodes[len(m.graph.Nodes)-1].ID
	case "pgup":
		m.graph.SelectNext(-1)
	case "pgdown":
		m.graph.SelectNext(1)
	}
	width, height := m.canvasSize()
	m.graph.Reveal(width, height)
}

func (m *Model) canvasSize() (int, int) {
	view := m.presentation()
	area := view.Panes(m.inspectorOpen).Main
	layout := view.Layout(area.Width, false)
	return max(layout.Body.Width, 1), max(layout.Body.Height, 1)
}

func (m *Model) content() string {
	view := m.presentation()
	cw, ch := m.canvasSize()
	m.ensureSelection()
	if m.run == nil {
		m.graph = m.buildGraph(m.graph)
	}
	canvas := m.graph
	if m.overview {
		canvas = m.graph.Overview(cw)
	}
	canvas.Nodes = append([]graph.Node(nil), canvas.Nodes...)
	for i := range canvas.Nodes {
		canvas.Nodes[i].Detail, canvas.Nodes[i].DetailTone = CardDetail(canvas.Nodes[i].Detail)
	}
	left := canvas.View(cw, ch, view.Styles.Graph)
	if len(canvas.Nodes) == 0 {
		left = cellFrame("No workflow open.\nCtrl+O opens a recipe · H saved runs\nA adds a node · I requests an AI proposal", cw, ch)
	}
	title := m.editor.Definition.Name
	if m.editor.Dirty() {
		title += " *"
	}
	if m.run != nil {
		title = m.run.Definition.Name + " / " + m.run.Status
	}
	mode := "Workflow"
	if m.positionsLocked {
		mode += " [positions locked]"
	}
	regions := view.Panes(m.inspectorOpen)
	split := m.inspectorOpen && !regions.Covered
	leftWidth := regions.Main.Width
	footerWidth := view.Layout(leftWidth, false).Footer.Width
	rows := view.Render(screen.Panel{
		Title: mode + " / " + cleanText(title),
		Hint:  pane.NoticeRow(cleanText(m.notice), m.noticeLevel, footerWidth, view.Styles.Notice),
		Body:  left, Width: leftWidth, Focused: !m.inspect,
	})
	if m.inspectorOpen {
		width := regions.Side.Width
		content := m.inspectorContent()
		tabs := m.inspectorTabs()
		capacity := max(1, ch-len(tabs.Rows)-1)
		m.offset = min(m.offset, max(0, len(content)-capacity))
		visible := content[m.offset:min(len(content), m.offset+capacity)]
		inner := view.Layout(width, true).Body.Width
		body := strings.Join(tabs.Rows, "\n") + "\n\n" + strings.Join(popup.ScrollRows(visible, inner, m.offset, len(content), view.Styles.Muted), "\n")
		heading := "Inspect / " + cleanText(m.graph.Selected)
		if m.selectedSchedule() != nil && m.run == nil {
			heading = "Inspect / Schedule"
		}
		if m.rawDetails {
			heading += " / Raw"
		}
		if m.inspect {
			heading += " [focused]"
		}
		shortcuts := []popup.Shortcut{{Key: "Tab", Action: "focus inspector"}, {Key: "1–3", Action: "tabs"}}
		if m.inspect {
			shortcuts = []popup.Shortcut{{Key: "Esc", Action: "close"}, {Key: "Tab", Action: "parent"}, {Key: "1–3", Action: "tabs"}}
		}
		if m.inspect && m.run == nil && !m.busy && m.inspectorTab != 1 {
			shortcuts = []popup.Shortcut{{Key: "Esc", Action: "close"}, {Key: "Tab", Action: "parent"}, {Key: "E", Action: "edit"}, {Key: "1–3", Action: "tabs"}}
		}
		right := view.Render(screen.Panel{
			Title: heading, Hint: view.Hints(width, shortcuts...), Body: body,
			Width: width, Focused: m.inspect, Close: true,
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

func (m *Model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	view := m.presentation()

	cw, ch := m.canvasSize()
	regions := view.Panes(m.inspectorOpen)
	canvasLayout := view.Layout(regions.Main.Width, false)
	sideLayout := view.Layout(regions.Side.Width, true)
	x, y := mouse.X-canvasLayout.Body.X, mouse.Y-regions.Main.Y-canvasLayout.Body.Y
	inspectorHeader := len(m.inspectorTabs().Rows) + 1
	inInspector := m.inspectorOpen && regions.Side.Contains(mouse.X, mouse.Y)
	if inInspector && sideLayout.CloseHit.Contains(mouse.X-regions.Side.X, mouse.Y-regions.Side.Y) && mouse.Button == tea.MouseLeft && isMouseClick(msg) {
		m.inspectorOpen, m.inspect = false, false
		m.thumb = scrollDrag{}
		return m, nil
	}
	if m.thumb.active {
		m.offset = popup.ScrollOffset(y-inspectorHeader, max(1, ch-inspectorHeader), len(m.inspectorContent()), m.thumb.grab)
		if _, ok := msg.(tea.MouseReleaseMsg); ok {
			m.thumb = scrollDrag{}
		}
		return m, nil
	}
	if inInspector && mouse.Button == tea.MouseLeft && isMouseClick(msg) {
		tabs := m.inspectorTabs()
		if tab := tabs.Hit(mouse.X-regions.Side.X-sideLayout.Body.X, y); tab >= 0 {
			m.setInspectorTab(tab)
			m.inspect = true
			return m, nil
		}
	}

	if _, ok := msg.(tea.MouseReleaseMsg); ok {
		dragged := m.dragging
		m.dragging = false
		m.panning = false
		if dragged {
			m.persistLayout()
		}
		return m, nil
	}
	if m.panning {
		m.graph.Pan.X -= x - m.last.X
		m.graph.Pan.Y -= y - m.last.Y
		m.last = graph.Point{X: x, Y: y}
		return m, nil
	}
	if m.dragging {
		m.graph.Move(x-m.last.X, y-m.last.Y)
		m.last = graph.Point{X: x, Y: y}
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
				m.offset = max(0, m.offset+delta)
			}
		} else if mouse.Button == tea.MouseWheelLeft || mouse.Button == tea.MouseWheelRight || mouse.Mod&tea.ModShift != 0 {
			m.graph.Pan.X += delta
		} else {
			m.graph.Pan.Y += delta
		}
		return m, nil
	}
	if inInspector {
		if mouse.Button == tea.MouseLeft && isMouseClick(msg) {
			m.inspect = true
			if y >= inspectorHeader && y < ch && mouse.X == regions.Side.X+sideLayout.Body.X+sideLayout.Body.Width-1 {
				total := len(m.inspectorContent())
				top, size := popup.ScrollThumb(max(1, ch-inspectorHeader), total, m.offset)
				if y-inspectorHeader >= top && y-inspectorHeader < top+size {
					m.thumb = scrollDrag{active: true, grab: y - inspectorHeader - top}
				} else {
					m.offset = popup.ScrollOffset(y-inspectorHeader, max(1, ch-inspectorHeader), total, 0)
				}
			}
		}
		return m, nil
	}
	if x < 0 || x >= cw {
		return m, nil
	}
	if mouse.Button == tea.MouseMiddle && isMouseClick(msg) {
		m.panning = true
		m.last = graph.Point{X: x, Y: y}
		return m, nil
	}
	if mouse.Button == tea.MouseLeft && isMouseClick(msg) {
		canvas := m.graph
		if m.overview {
			canvas = m.graph.Overview(cw)
		}
		if !m.overview {
			if id, port, output := canvas.PortHit(x, y); id != "" {
				m.graph.Selected = id
				if strings.HasPrefix(id, workflowSchedulePrefix) || strings.HasPrefix(m.connectFrom, workflowSchedulePrefix) {
					m.connectFrom = ""
					m.inspectorOpen = true
					m.setNotice(pane.NoticeInfo, "Schedule starts the whole recipe; its connections are automatic")
					return m, nil
				}
				m.focusPort(port, output)
				m.inspect = false
				if m.run != nil || m.busy {
					m.inspectorOpen = true
					m.inspect = true
					return m, nil
				}
				if output {
					m.connectFrom = id
					m.connectPort = port
					m.setNotice(pane.NoticeInfo, "Choose an input port · Esc cancels")
				} else if m.connectFrom != "" {
					if (port == "@order") != (m.connectPort == "@order") {
						m.setNotice(pane.NoticeWarning, "Connect data ports together, or then → after for execution order")
						return m, nil
					}
					edit := workflowConnectionEdit{From: m.connectFrom, To: id, Pointer: m.connectPort, FixedSource: true, Draft: m.editor.Source()}
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
						m.setNotice(pane.NoticeWarning, "Choose a named destination port")
						return m, nil
					}
					m.connectFrom = ""
					m.connectionEdit(edit)
				} else {
					m.setNotice(pane.NoticeWarning, "Choose an output port first · C connects by keyboard")
				}
				return m, nil
			}
		}
		id := canvas.Hit(x, y)
		m.inspect = false
		if id != "" {
			m.graph.Selected = id
			m.portSelected = false
			m.offset = 0
			m.dragging = !m.overview && !m.positionsLocked
		} else {
			m.panning = !m.overview
		}
		m.last = graph.Point{X: x, Y: y}
	}
	return m, nil
}

// CardDetail presents an execution status without changing selection styling.
func CardDetail(status string) (string, graph.Tone) {
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

func (m *Model) View() tea.View { return tea.NewView("\n" + m.content()) }

func (m *Model) inspectorContent() []string {
	view := m.presentation()
	width := view.Layout(view.Panes(true).Side.Width, true).Body.Width
	var out []string
	for _, line := range m.detailRows()[1:] {
		out = append(out, strings.Split(ansi.Hardwrap(line, max(1, width-1), true), "\n")...)
	}
	return out
}
