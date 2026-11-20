package screen

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/pkg/tui/components/popup"
)

// DialogMouse handles pointer input for the shared overlay. Toolbar actions are
// routed by the application before reaching this method.
func (m *Controls) DialogMouse(msg tea.MouseMsg) tea.Cmd {
	if m.dialog == nil {
		return nil
	}
	if m.dialog.Table != nil {
		mouse := msg.Mouse()
		mouse.X -= m.Props.Area.X
		mouse.Y -= m.Props.Area.Y
		var local tea.MouseMsg
		switch msg.(type) {
		case tea.MouseClickMsg:
			local = tea.MouseClickMsg(mouse)
		case tea.MouseWheelMsg:
			local = tea.MouseWheelMsg(mouse)
		case tea.MouseMotionMsg:
			local = tea.MouseMotionMsg(mouse)
		case tea.MouseReleaseMsg:
			local = tea.MouseReleaseMsg(mouse)
		default:
			return nil
		}
		return m.TableAction(m.dialog.Table.model.Mouse(local, m.TableProps()))
	}
	if m.dialog.Form != nil {
		return m.FormMouse(msg)
	}
	mouse := msg.Mouse()
	if mouse.Button == tea.MouseLeft && IsMouseClick(msg) {
		rows, w, h := m.DialogRows()
		inner := m.dialogWidth(w) - 4
		x := m.Props.Area.X + max((w-m.dialogWidth(w))/2, 0) + 2
		y := m.Props.Area.Y + max((h-len(rows)-2)/2, 0) + 1
		if mouse.Y == y && mouse.X >= x+inner-3 && mouse.X < x+inner {
			m.Close()
			return nil
		}
		if m.dialog.Help {
			layout := m.LayoutDialog()
			if tab := layout.Tabs.Hit(mouse.X-x, mouse.Y-y-2); tab >= 0 {
				m.dialog.Page = tab
				m.dialog.Offset = 0
				m.dialog.Drag = popup.ScrollDrag{}
				return nil
			}
		}
	}
	if m.DialogScrollbarMouse(msg) {
		return nil
	}
	switch mouse.Button {
	case tea.MouseWheelDown:
		m.ScrollDialog(1)
	case tea.MouseWheelUp:
		m.ScrollDialog(-1)
	}
	return nil
}
