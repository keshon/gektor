package agent

import (
	"strconv"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/table"
)

type tableDialog struct {
	model        *table.Model
	list         bool
	refresh      func()
	columns      []table.Column
	description  string
	emptyMessage string
	actions      func() []table.Action
	activate     func(action, id string) tea.Cmd
}

func (m *Model) tableProps() table.Props {
	d := m.dialog.table
	if d.refresh != nil {
		d.refresh()
	}
	d.model.Search.SetStyles(textInputStyles(d.model.Search.Styles()))
	_, _, width, height := m.settingsArea()
	return table.Props{Title: m.dialog.title, Description: d.description, EmptyMessage: d.emptyMessage, Width: width, Height: height, DefaultAction: "details", List: d.list,
		Columns: d.columns, Actions: d.actions(), Surface: popupSurface(),
		Styles: form.Styles{Normal: popupSurface().Normal, Title: m.styles.title, Muted: m.styles.dim,
			Focus: m.styles.focus, Disabled: m.styles.disabled, Error: m.styles.err}}
}

func (m *Model) tableInput(msg tea.Msg) tea.Cmd {
	action, cmd := m.dialog.table.model.Update(msg, m.tableProps())
	return tea.Batch(cmd, m.tableAction(action))
}

func (m *Model) tableAction(action string) tea.Cmd {
	if action == "" {
		return nil
	}
	if action == "close" {
		m.closeDialog()
		return nil
	}
	d := m.dialog.table
	id := d.model.Selected()
	return d.activate(action, id)
}

func (m *Model) newTable() *table.Model {
	return table.New(textInputStyles(textinput.New().Styles()))
}

func (m *Model) openPermissions() {
	rows := []table.Row{}
	for i, rule := range m.policyDetails {
		rows = append(rows, table.Row{ID: strconv.Itoa(i), Cells: []string{strconv.Itoa(i + 1), rule.Effect.String(), rule.Tool, rule.Pattern}})
	}
	view := m.newTable()
	view.SetRows(rows)
	m.openPermissionTable(view)
}

func (m *Model) openPermissionTable(view *table.Model) {
	m.openDialog("Permissions", nil)
	m.dialog.table = &tableDialog{model: view,
		columns:     []table.Column{{Label: "Order", Width: 5}, {Label: "Decision", Width: 8}, {Label: "Tool", Width: 18}, {Label: "Pattern"}},
		description: "Later matching rules take precedence. Plan uses read-only tools. Act follows rules and approval prompts. This view does not change permissions.",
		actions: func() []table.Action {
			return []table.Action{{ID: "details", Label: "Details", Key: "enter", Enabled: view.Selected() != ""}, {ID: "close", Label: "Close", Key: "esc", Enabled: true}}
		},
		activate: func(action, id string) tea.Cmd {
			if action != "details" {
				return nil
			}
			i, err := strconv.Atoi(id)
			if err != nil || i < 0 || i >= len(m.policyDetails) {
				return nil
			}
			rule := m.policyDetails[i]
			m.closeDialog()
			m.openDialog("Permission rule "+strconv.Itoa(i+1), []string{"Decision: " + rule.Effect.String(), "Tool: " + rule.Tool, "Pattern: " + rule.Pattern, "", "Later matching rules take precedence."})
			m.dialog.onClose = func() {
				m.pendingCommands = append(m.pendingCommands, func() tea.Msg { return permissionTableMsg{view} })
			}
			return nil
		}}
}

type permissionTableMsg struct{ view *table.Model }
