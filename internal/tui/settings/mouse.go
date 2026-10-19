package settings

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/tui/components/cell"
)

func (f *Model) Mouse(msg tea.MouseMsg, props Props) (Intent, tea.Cmd) {
	if f.Picker != nil && (msg.Mouse().Button == tea.MouseWheelUp || msg.Mouse().Button == tea.MouseWheelDown) {
		p := f.Picker
		step := 1
		if msg.Mouse().Button == tea.MouseWheelUp {
			step = -1
		}
		p.Move(step, len(p.Filtered), 1)
		return Intent{}, nil
	}
	if msg.Mouse().Button != tea.MouseLeft || !isClick(msg) {
		return Intent{}, nil
	}
	x, y := f.Origin(props)
	_, hits, _, _ := f.Rows(props)
	width := cell.Width(strings.Join(f.box(props), "\n"))
	mouse := msg.Mouse()
	if f.Picker != nil {
		p := f.Picker
		row := mouse.Y - y - 3
		capacity := max(props.Height-10, 1)
		start := max(min(p.Cursor-capacity/2, len(p.Filtered)-capacity), 0)
		if mouse.X >= x-2 && mouse.X < x-2+width && row >= 0 && row < capacity && start+row < len(p.Filtered) {
			p.Cursor = start + row
			cmd := f.PickerKey(tea.KeyPressMsg{Code: tea.KeyEnter}, props)
			return Intent{}, cmd
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
			if hit.Index < len(f.Fields) && mouse.Y > y+hit.Y0 && (len(f.Options(f.Fields[hit.Index].Key)) > 0 || PickerControl(f.Fields[hit.Index].Key)) {
				return Intent{}, f.OpenPicker(props)
			}
			if hit.Index == len(f.Fields) {
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
