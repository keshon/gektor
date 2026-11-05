package tui

import "github.com/keshon/gektor/internal/tui/components/cell"

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
func (m *model) paneHeight() int { return max(m.termH-4-m.footerRows(), 1) }
func (m *model) paneWidth() int {
	inset := 2 * m.promptOffset()
	// The inspector supplies the single shared gutter in borderless split view.
	if m.minimal && m.inspectorVisible() && m.termW >= inspectorSplitWidth {
		inset = m.promptOffset()
	}
	return max(m.mainWidth()-inset, 0)
}

const paneTop = 4
