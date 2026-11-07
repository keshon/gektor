package settings

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

func (f *Model) Mouse(msg tea.MouseMsg, props Props) (Intent, tea.Cmd) {
	if msg.Mouse().Button == tea.MouseWheelUp || msg.Mouse().Button == tea.MouseWheelDown {
		x, y := f.Origin(props)
		box := f.box(props)
		width := cell.Width(strings.Join(box, "\n"))
		mouse := msg.Mouse()
		if mouse.X < x-2 || mouse.X >= x-2+width || mouse.Y < y-1 || mouse.Y >= y-1+len(box) {
			return Intent{}, nil
		}
	}
	if f.Picker != nil && (msg.Mouse().Button == tea.MouseWheelUp || msg.Mouse().Button == tea.MouseWheelDown) {
		p := f.Picker
		l := f.layoutPicker(props)
		x, y := f.Origin(props)
		mouse := msg.Mouse()
		if mouse.X < x || mouse.X >= x+l.inner || mouse.Y < y+l.listTop || mouse.Y >= y+l.listTop+min(l.capacity, len(p.Filtered)) {
			return Intent{}, nil
		}
		step := 1
		if msg.Mouse().Button == tea.MouseWheelUp {
			step = -1
		}
		p.Move(step, len(p.Filtered), 1)
		return Intent{}, nil
	}
	if msg.Mouse().Button != tea.MouseLeft || !isClick(msg) {
		if f.Picker == nil && (msg.Mouse().Button == tea.MouseWheelUp || msg.Mouse().Button == tea.MouseWheelDown) {
			key := tea.KeyDown
			if msg.Mouse().Button == tea.MouseWheelUp {
				key = tea.KeyUp
			}
			return f.UpdateKey(tea.KeyPressMsg{Code: key}, props)
		}
		return Intent{}, nil
	}
	x, y := f.Origin(props)
	_, hits, _, _ := f.Rows(props)
	width := cell.Width(strings.Join(f.box(props), "\n"))
	mouse := msg.Mouse()
	if f.Picker != nil {
		p := f.Picker
		for _, hit := range hits {
			if hit.Index == -8 && mouse.Y == y+hit.Y0 && mouse.X >= x+hit.X0 && mouse.X < x+hit.X1 {
				p.Control = 0
				return Intent{}, nil
			}
			if hit.Index == -5 && mouse.Y == y+hit.Y0 && mouse.X >= x+hit.X0 && mouse.X < x+hit.X1 {
				p.Control = 0
				p.Cursor = hit.Option - 1
				if p.Browser && strings.HasSuffix(p.Filtered[p.Cursor], string(os.PathSeparator)) {
					return Intent{}, f.PickerKey(tea.KeyPressMsg{Code: tea.KeyEnter}, props)
				}
				return Intent{}, nil
			}
			if hit.Index == -7 && mouse.Y == y+hit.Y0 && mouse.X >= x+hit.X0 && mouse.X < x+hit.X1 && !p.Loading {
				p.Control = 0
				return Intent{}, f.PickerKey(tea.KeyPressMsg{Code: tea.KeyEnter}, props)
			}
			if hit.Index == -4 && mouse.Y == y+hit.Y0 && mouse.X >= x+hit.X0 && mouse.X < x+hit.X1 {
				f.Picker = nil
				return Intent{}, nil
			}
			if hit.Index == -3 && mouse.Y == y && mouse.X >= x+hit.X0 && mouse.X < x+hit.X1 {
				return Intent{}, f.PickerKey(tea.KeyPressMsg{Code: tea.KeyEscape}, props)
			}
			if hit.Index == -2 && mouse.Y == y+hit.Y0 && mouse.X >= x+hit.X0 && mouse.X < x+hit.X1 {
				return Intent{}, f.PickerKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, props)
			}
		}
		l := f.layoutPicker(props)
		row := mouse.Y - y - l.listTop
		capacity := l.capacity
		if mouse.X == x+l.inner-1 && row >= 0 && row < capacity && len(p.Filtered) > capacity {
			p.Cursor = min(popup.ScrollOffset(row, capacity, len(p.Filtered), 0)+capacity/2, len(p.Filtered)-1)
			return Intent{}, nil
		}
		return Intent{}, nil
	}
	if mouse.X < x-2 || mouse.X >= x-2+width {
		return Intent{}, nil
	}
	for _, hit := range hits {
		if mouse.Y >= y+hit.Y0 && mouse.Y < y+hit.Y1 {
			if hit.X1 > 0 && (mouse.X < x+hit.X0 || mouse.X >= x+hit.X1) {
				continue
			}
			f.Focus(hit.Index, f.FieldVisible)
			if hit.Option > 0 {
				options := f.inlineOptions(f.Fields[hit.Index])
				f.setChoice(options[hit.Option-1].Value)
				return Intent{}, nil
			}
			if hit.Index < len(f.Fields) && mouse.Y > y+hit.Y0 && (len(f.Options(f.Fields[hit.Index].Key)) > 0 || f.PickerControl(f.Fields[hit.Index].Key)) {
				if len(f.inlineOptions(f.Fields[hit.Index])) > 0 {
					return Intent{}, nil
				}
				return Intent{}, f.OpenPicker(props)
			}
			if hit.Index == len(f.Fields) {
				if !f.Dirty() {
					return Intent{}, nil
				}
				return Intent{Action: Apply}, nil
			} else if hit.Index == len(f.Fields)+1 {
				return Intent{Action: Cancel}, nil
			}
			break
		}
	}
	return Intent{}, nil
}

func isClick(msg tea.MouseMsg) bool { _, ok := msg.(tea.MouseClickMsg); return ok }
