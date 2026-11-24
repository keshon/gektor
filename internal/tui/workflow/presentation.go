package workflow

import (
	"github.com/keshon/gektor/pkg/tui/components/pane"
	"github.com/keshon/gektor/pkg/tui/screen"
)

// Presentation supplies local controls and overlay bounds for the active screen.
func (m *Model) Presentation() screen.ControlProps {
	surface := m.surface
	layout := surface.Layout(surface.Area.Width, false)
	props := screen.ControlProps{
		Ready:  true,
		Styles: screen.DefaultStyles(),
		Area: pane.Rect{
			X:      surface.Area.X + layout.Body.X,
			Y:      surface.Area.Y + layout.Body.Y,
			Width:  layout.Body.Width,
			Height: layout.Body.Height,
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

func (m *Model) viewSurface() screen.Surface {
	surface := m.surface
	if m.inspectorExpanded {
		surface.InspectorWidth = surface.Area.Width
	}
	return surface
}
