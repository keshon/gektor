package agent

import (
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/pkg/tui/components/cell"
	"github.com/keshon/gektor/pkg/tui/components/pane"
)

func cellLine(s string, width int) string          { return cell.Line(s, width) }
func cellFrame(s string, width, height int) string { return cell.Frame(s, width, height) }
func (m *Model) fitColumns()                       { m.transcript.Viewport.SetWidth(max(m.paneWidth(), 1)) }
func (m *Model) identityLine() string              { return m.badgeLine() }
func (m *Model) promptOffset() int {
	if m.termW < 40 || m.termH < 14 {
		return 0
	}
	return 1
}
func (m *Model) paneWidth() int {
	return m.panelGeometry(m.mainWidth(), 2, false, true).Body.Width
}

func (m *Model) screenPanes(sideOpen bool) pane.SplitLayout {
	area := pane.Rect{Y: 1, Width: m.termW, Height: max(m.termH-1-m.actionBarRows(), 1)}
	props := pane.SplitProps{MinimumWidth: inspectorSplitWidth}
	if m.inspector.expanded {
		props.SideWidth = area.Width
	}
	return pane.Split(area, sideOpen, props)
}

func (m *Model) panelGeometry(width, footerHeight int, close, conversation bool) pane.Layout {
	right := m.promptOffset()
	if conversation && m.minimal && m.inspectorVisible() && !m.screenPanes(true).Covered {
		right = 0
	}
	return pane.Measure(pane.Props{Width: width, Height: max(m.termH-1-m.actionBarRows(), 1), FooterHeight: footerHeight,
		LeftInset: m.promptOffset(), RightInset: right, Outlined: !m.minimal && m.promptOffset() > 0,
		BottomEdge: m.promptOffset() > 0, Quiet: m.modern && m.minimal, Headerless: m.modern && conversation, Close: close})
}

func (m *Model) paneStyles() pane.Styles {
	return pane.Styles{Border: lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorPopupBorder))), Title: m.styles.Title, Focus: m.styles.Focus}
}

func (m *Model) transcriptTop() int {
	return 1 + m.panelGeometry(m.mainWidth(), len(m.composerRows()), false, true).Body.Y
}

func (m *Model) dialogArea() (left, top, width, height int) {
	return m.promptOffset(), m.transcriptTop(), m.transcript.Viewport.Width(), m.transcript.Viewport.Height()
}
