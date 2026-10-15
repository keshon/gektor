package settings

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	rows, hits, w, h := f.Rows(props)
	bw := 0
	for _, row := range rows {
		bw = max(bw, lipgloss.Width(row))
	}
	x := props.Left + max((w-bw-4)/2, 0)
	y := props.Top + max((h-len(rows)-2)/2, 0) + 1
	if f.Workspace != nil {
		x = 0
		y = 2
	}
	mouse := msg.Mouse()
	if f.Picker != nil {
		p := f.Picker
		row := mouse.Y - y - 3
		capacity := max(h-10, 1)
		start := max(min(p.Cursor-capacity/2, len(p.Filtered)-capacity), 0)
		if mouse.X >= x && mouse.X < x+bw+4 && row >= 0 && row < capacity && start+row < len(p.Filtered) {
			p.Cursor = start + row
			cmd := f.PickerKey(tea.KeyPressMsg{Code: tea.KeyEnter}, props)
			return Intent{}, cmd
		}
		return Intent{}, nil
	}
	if f.Workspace != nil && w >= 76 && mouse.X >= x+1 && mouse.X < x+17 {
		row := mouse.Y - y
		if row >= 0 && row < len(f.AvailableCategories()) {
			category := f.AvailableCategories()[row].ID
			if category != "speech" || f.HasSpeech() {
				if category != f.Workspace.Section {
					return Intent{Action: SwitchCategory, Section: category}, nil
				}
			}
		}
		if row >= 0 && row < len(f.AvailableCategories()) {
			return Intent{}, nil
		}
	}
	if mouse.X < x || mouse.X >= x+bw+4 {
		return Intent{}, nil
	}
	for _, hit := range hits {
		if mouse.Y >= y+hit.Y0 && mouse.Y < y+hit.Y1 {
			if hit.X1 > 0 {
				contentX := x + 2
				if f.Workspace != nil && w >= 76 {
					contentX += 17
				}
				if mouse.X < contentX+hit.X0 || mouse.X >= contentX+hit.X1 {
					continue
				}
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
