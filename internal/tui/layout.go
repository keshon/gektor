package tui

import "github.com/keshon/gektor/internal/tui/components/cell"

func cellLine(s string, width int) string          { return cell.Line(s, width) }
func cellFrame(s string, width, height int) string { return cell.Frame(s, width, height) }
func (m *model) fitColumns()                       { m.transcript.Viewport.SetWidth(max(m.paneWidth(), 1)) }
func (m *model) identityLine() string              { return m.badgeLine() }
func (m *model) promptOffset() int {
	if m.minimal || !m.actionBarVisible() {
		return 0
	}
	return 1
}
func (m *model) paneHeight() int { return max(m.termH-4-m.footerRows(), 1) }
func (m *model) paneWidth() int  { return max(m.termW-2*m.promptOffset(), 0) }

const paneTop = 4
