package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/pane"
)

func cellLine(s string, width int) string          { return cell.Line(s, width) }
func cellFrame(s string, width, height int) string { return cell.Frame(s, width, height) }
func (m *model) fitColumns()                       { m.transcript.Viewport.SetWidth(max(m.paneWidth(), 1)) }
func (m *model) identityLine() string              { return m.badgeLine() }
func (m *model) promptOffset() int {
	if !m.actionBarVisible() {
		return 0
	}
	return 1
}
func (m *model) paneWidth() int {
	return m.panelGeometry(m.mainWidth(), 2, false, true).Body.Width
}

func (m *model) screenPanes(sideOpen bool) pane.SplitLayout {
	return pane.Split(pane.Rect{Y: 1, Width: m.termW, Height: max(m.termH-1-m.actionBarRows(), 1)}, sideOpen, inspectorSplitWidth)
}

func (m *model) panelGeometry(width, footerHeight int, close, conversation bool) pane.Layout {
	right := m.promptOffset()
	if conversation && m.minimal && m.inspectorVisible() && m.termW >= inspectorSplitWidth {
		right = 0
	}
	return pane.Measure(pane.Props{Width: width, Height: max(m.termH-1-m.actionBarRows(), 1), FooterHeight: footerHeight,
		LeftInset: m.promptOffset(), RightInset: right, Outlined: !m.minimal && m.promptOffset() > 0,
		BottomEdge: m.actionBarVisible(), Close: close})
}

func (m *model) paneStyles() pane.Styles {
	return pane.Styles{Border: lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorPopupBorder))), Title: m.styles.title, Focus: m.styles.focus}
}

const paneTop = 4
