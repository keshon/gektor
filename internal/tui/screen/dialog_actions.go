package screen

import tea "charm.land/bubbletea/v2"

// DialogActions supplies shortcuts for the shared form or table currently open.
// Information overlays retain the active screen's normal navigation shortcuts.
func (m *Controls) DialogActions() []Action {
	if m.Dialog != nil && m.Dialog.Table != nil {
		props := m.TableProps()

		usable := m.Dialog.Table.Model.Layout(props).Capacity > 0
		enter := Action{Key: tea.KeyEnter, Mod: 0, Name: "Enter", Label: "Choose", Short: "Choose", Enabled: false}
		for _, action := range props.Actions {
			if action.ID == props.DefaultAction {
				enter.Label, enter.Short, enter.Enabled = action.Label, action.Label, action.Enabled
			}
		}
		if m.Dialog.Table.Model.MoreOpen {
			return []Action{{Key: tea.KeyEnter, Mod: 0, Name: "Enter", Label: "Choose", Short: "Choose", Enabled: true}, {Key: tea.KeyEscape, Mod: 0, Name: "Esc", Label: "Back", Short: "Back", Enabled: true}, {Key: 'q', Mod: tea.ModCtrl, Name: "Ctrl+Q", Label: "Quit", Short: "Quit", Enabled: true}}
		}
		if !usable {
			enter = Action{Key: tea.KeyEnter, Mod: 0, Name: "Enter", Label: "Close", Short: "Close", Enabled: true}
		}
		if props.List {
			return []Action{enter, {Key: tea.KeyEscape, Mod: 0, Name: "Esc", Label: "Close", Short: "Close", Enabled: true}}
		}
		return []Action{{Key: '/', Mod: 0, Name: "/", Label: "Filter", Short: "Filter", Enabled: usable}, {Key: tea.KeyTab, Mod: 0, Name: "Tab", Label: "Controls", Short: "Next", Enabled: usable},
			enter,
			{Key: tea.KeyEscape, Mod: 0, Name: "Esc", Label: "Back", Short: "Back", Enabled: true}, {Key: 'q', Mod: tea.ModCtrl, Name: "Ctrl+Q", Label: "Quit", Short: "Quit", Enabled: true}}
	}
	if m.Dialog != nil && m.Dialog.Form != nil {
		if m.Dialog.Form.Picker != nil {
			p := m.Dialog.Form.Picker
			label, enabled := p.EnterAction()
			enter := Action{Key: tea.KeyEnter, Mod: 0, Name: "Enter", Label: label, Short: label, Enabled: enabled}
			if p.Field == "theme" {
				return []Action{enter, {Key: tea.KeyEscape, Mod: 0, Name: "Esc", Label: "Back", Short: "Back", Enabled: true}}
			}
			if p.Field == "app-workspace" {
				return []Action{enter, {Key: 's', Mod: tea.ModCtrl, Name: "Ctrl+S", Label: "Select folder", Short: "Select", Enabled: !p.Location && !p.Loading && p.Err == "" && p.Directory != ""}, {Key: 'l', Mod: tea.ModCtrl, Name: "Ctrl+L", Label: "Path", Short: "Path", Enabled: true}, {Key: 'd', Mod: tea.ModCtrl, Name: "Ctrl+D", Label: "Drives", Short: "Drives", Enabled: true}, {Key: tea.KeyEscape, Mod: 0, Name: "Esc", Label: "Back", Short: "Back", Enabled: true}}
			}
			return []Action{enter, {Key: tea.KeyEscape, Mod: 0, Name: "Esc", Label: "Back", Short: "Back", Enabled: true}, {Key: 'b', Mod: tea.ModCtrl, Name: "Ctrl+B", Label: "Browse", Short: "Browse", Enabled: m.Dialog.Form.Picker.IsFiles()}, {Key: 'r', Mod: tea.ModCtrl, Name: "Ctrl+R", Label: "Refresh", Short: "Refresh", Enabled: true}, {Key: 'q', Mod: tea.ModCtrl, Name: "Ctrl+Q", Label: "Quit", Short: "Quit", Enabled: true}}
		}
		label := m.Dialog.Form.ApplyLabel
		if label == "" {
			label = "Apply"
		}
		apply := Action{Key: 's', Mod: tea.ModCtrl, Name: "Ctrl+S", Label: label, Short: label, Enabled: m.Dialog.Form.EditSession == nil || m.Dialog.Form.Dirty()}
		if m.Dialog.Form.Destructive {
			apply = Action{Key: tea.KeyEnter, Mod: 0, Name: "Enter", Label: "Choose", Short: "Choose", Enabled: true}
		}
		if ws := m.Dialog.Form.EditSession; ws != nil {
			// F6 was the in-form category switch. With one section per
			// modal there is nothing for it to switch, and a footer entry
			// that does nothing is worse than no entry.
			actions := []Action{{Key: tea.KeyF4, Mod: 0, Name: "F4", Label: "Advanced", Short: "More", Enabled: true}, {Key: tea.KeyTab, Mod: 0, Name: "Tab", Label: "Next", Short: "Next", Enabled: true}, apply, {Key: tea.KeyEscape, Mod: 0, Name: "Esc", Label: "Cancel", Short: "Cancel", Enabled: true}}
			if ws.Section == "profiles" {
				actions = append(actions, Action{Key: 'd', Mod: tea.ModCtrl, Name: "Ctrl+D", Label: "Save copy", Short: "Save", Enabled: true})
			}
			return actions
		}
		if m.Props.Width < 64 {
			return []Action{apply, {Key: tea.KeyEscape, Mod: 0, Name: "Esc", Label: "Cancel", Short: "Cancel", Enabled: true}}
		}
		return []Action{{Key: tea.KeyTab, Mod: 0, Name: "Tab", Label: "Next", Short: "Next", Enabled: true}, apply, {Key: tea.KeyEscape, Mod: 0, Name: "Esc", Label: "Cancel", Short: "Cancel", Enabled: true}, {Key: 'q', Mod: tea.ModCtrl, Name: "Ctrl+Q", Label: "Quit", Short: "Quit", Enabled: true}}
	}
	return nil
}
