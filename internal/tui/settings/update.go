package settings

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/form"
)

type Action int

const (
	None Action = iota
	Apply
	Cancel
	SaveCopy
)

// Intent describes an application action; the component never applies services.
type Intent struct {
	Action  Action
	Section string
}

func (f *Model) UpdateKey(msg tea.KeyPressMsg, props Props) (Intent, tea.Cmd) {
	if f.Picker != nil {
		return Intent{}, f.PickerKey(msg, props)
	}
	if ws := f.EditSession; ws != nil {
		switch msg.String() {
		case "ctrl+d":
			if ws.Section == "profiles" {
				return Intent{Action: SaveCopy}, nil
			}
		case "f4":
			ws.Advanced = !ws.Advanced
			f.Focus(f.FirstVisibleField(), f.FieldVisible)
			return Intent{}, nil
		}
	}
	if f.Index < len(f.Fields) && len(f.inlineOptions(f.Fields[f.Index])) > 0 {
		switch msg.String() {
		case "left":
			f.cycleChoice(-1)
			return Intent{}, nil
		case "right", "space", "enter":
			f.cycleChoice(1)
			return Intent{}, nil
		case "tab", "shift+tab", "up", "down", "ctrl+up", "ctrl+down", "ctrl+s", "esc":
			// Form navigation and actions retain their usual keys.
		default:
			return Intent{}, nil
		}
	}
	choice := f.Index < len(f.Fields) && (len(f.Options(f.Fields[f.Index].Key)) > 0 || f.PickerControl(f.Fields[f.Index].Key))
	intent, cmd := f.Model.Update(msg, f.FieldVisible, choice, f.Destructive)
	switch intent {
	case form.Apply:
		if !f.Dirty() {
			return Intent{}, cmd
		}
		return Intent{Action: Apply}, cmd
	case form.Cancel:
		return Intent{Action: Cancel}, cmd
	case form.Pick:
		return Intent{}, f.OpenPicker(props)
	}
	return Intent{}, cmd
}
