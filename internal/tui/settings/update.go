package settings

import (
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/tui/components/form"
)

type Action int

const (
	None Action = iota
	Apply
	Cancel
	SaveCopy
	SwitchCategory
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
	if ws := f.Workspace; ws != nil {
		switch msg.String() {
		case "ctrl+d":
			if ws.Section == "profiles" {
				return Intent{Action: SaveCopy}, nil
			}
		case "f6":
			ws.Nav = !ws.Nav
			return Intent{}, nil
		case "f4":
			ws.Advanced = !ws.Advanced
			f.Focus(f.FirstVisibleField(), f.FieldVisible)
			return Intent{}, nil
		}
		if ws.Nav {
			return f.categoryKey(msg), nil
		}
	}
	choice := f.Index < len(f.Fields) && (len(f.Options(f.Fields[f.Index].Key)) > 0 || PickerControl(f.Fields[f.Index].Key))
	intent, cmd := f.Model.Update(msg, f.FieldVisible, choice, f.Destructive)
	switch intent {
	case form.Apply:
		return Intent{Action: Apply}, cmd
	case form.Cancel:
		return Intent{Action: Cancel}, cmd
	case form.Pick:
		return Intent{}, f.OpenPicker(props)
	}
	return Intent{}, cmd
}

func (f *Model) categoryKey(msg tea.KeyPressMsg) Intent {
	ws := f.Workspace
	switch msg.String() {
	case "ctrl+s":
		return Intent{Action: Apply}
	case "esc", "enter", "right", "tab":
		ws.Nav = false
	case "up", "down":
		categories := f.AvailableCategories()
		if len(categories) == 0 {
			return Intent{}
		}
		index := 0
		for i, c := range categories {
			if c.ID == ws.Section {
				index = i
			}
		}
		step := 1
		if msg.String() == "up" {
			step = -1
		}
		index = (index + step + len(categories)) % len(categories)
		if categories[index].ID != ws.Section {
			return Intent{Action: SwitchCategory, Section: categories[index].ID}
		}
	}
	return Intent{}
}
