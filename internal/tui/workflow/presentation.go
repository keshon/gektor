package workflow

import (
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
)

// Presentation supplies local controls and overlay bounds for the active screen.
func (m *Model) Presentation() screen.ControlProps {
	surface := m.surface
	_, height := m.canvasSize()
	props := screen.ControlProps{
		Ready:  true,
		Styles: screen.DefaultStyles(),
		Area: pane.Rect{
			X:      surface.Inset,
			Y:      surface.Area.Y + 3,
			Width:  max(1, surface.Area.Width-2*surface.Inset),
			Height: height,
		},
		Actions: m.footerActions(),
	}
	for _, group := range Menus() {
		group.Items = m.MenuItems(group.ID, true)
		for i := range group.Items {
			_, group.Items[i].Checked = m.MenuToggle(group.Items[i].Command)
			group.Items[i].Shortcut = MenuShortcut(group.Items[i].Command)
		}
		props.Menus = append(props.Menus, group)
	}
	return props
}
