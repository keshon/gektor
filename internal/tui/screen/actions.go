package screen

import "github.com/keshon/gektor/internal/tui/components/table"

// OpenActions uses the same labels, shortcuts and availability as the menus.
// Activation reports a TableAction whose Row is the selected menu command.
func (m *Controls) OpenActions(exclude string) {
	state := m.NewTable()
	model := state.model
	model.Search.Placeholder = "Type an action"
	seen := map[string]bool{}
	for _, menu := range m.Props.Menus {
		for _, item := range menu.Items {
			if item.Command == "" || item.Command == exclude || !item.Enabled || seen[item.Command] {
				continue
			}
			seen[item.Command] = true
			model.Rows = append(model.Rows, table.Row{
				ID:    item.Command,
				Cells: []string{menu.Label + " / " + item.Text, item.Shortcut},
			})
		}
	}
	model.Focus = 1
	model.SyncFocus(true)
	m.PresentDialog(Dialog{Title: "Actions", Table: &Table{
		State:   state,
		Columns: []table.Column{{Label: "Action"}, {Label: "Key", Width: 12, HideNarrow: true}},
		Actions: []table.Action{{ID: "details", Label: "Run", Key: "enter", Enabled: len(model.Rows) > 0}, {ID: "close", Label: "Cancel", Key: "esc", Enabled: true}},
	}})
}
