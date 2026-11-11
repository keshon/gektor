package workflow

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

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
