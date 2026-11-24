package screen

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/pkg/tui/components/cell"
	"github.com/keshon/gektor/pkg/tui/components/popup"
)

// dialogCursor uses the same rows and frame dimensions as overlayContent.
func (m *Controls) dialogCursor() *tea.Cursor {
	if m.dialog == nil || m.Header.Focused {
		return nil
	}
	if m.dialog.Form != nil {
		return m.dialog.Form.Cursor(m.FormProps())
	}
	var caret *tea.Cursor
	if m.dialog.Table != nil {
		caret = m.dialog.Table.model.Layout(m.TableProps()).Cursor
	} else if m.dialog.Secret != nil {
		layout := m.LayoutDialog()
		s := m.dialog.Secret
		row := 0
		for _, line := range m.dialog.Lines[:2] {
			row += len(cell.Wrap(cleanText(line), layout.Inner-1))
		}
		row -= m.dialog.Offset
		if row < 0 || row >= layout.Capacity {
			return nil
		}
		caret = s.editor.Cursor()
		if caret != nil {
			caret.Y += len(layout.Rows) + row
		}
	}
	if caret == nil {
		return nil
	}
	rows, width, height := m.DialogRows()
	framed := popup.ModalBox(rows, width, m.dialogWidth(width), m.popupSurface())
	frameWidth := cell.Width(strings.Join(framed, "\n"))
	left, top, _, _ := m.Area()
	if !m.Props.Ready {
		left, top = 0, 0
	}
	caret.X += left + max((width-frameWidth)/2, 0) + 2
	caret.Y += top + max((height-len(framed))/2, 0) + 1
	if caret.X < left || caret.X >= left+width || caret.Y < top || caret.Y >= top+height {
		return nil
	}
	return caret
}
