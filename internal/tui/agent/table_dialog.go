package agent

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/table"
	"github.com/keshon/gektor/internal/tui/screen"
)

type tableDialog = screen.TableState

func (m *Model) tableProps() table.Props {
	m.syncControls()
	return m.chrome.TableProps()
}

func (m *Model) tableInput(msg tea.Msg) tea.Cmd {
	m.syncControls()
	return m.chrome.TableInput(msg)
}

func (m *Model) tableAction(action string) tea.Cmd {
	m.syncControls()
	return m.chrome.TableAction(action)
}

func (m *Model) newTable() *table.Model {
	m.syncControls()
	return m.chrome.NewTable()
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
	m.chrome.Dialog.Table = &tableDialog{Model: view,
		Columns:     []table.Column{{Label: "Order", Width: 5}, {Label: "Decision", Width: 8}, {Label: "Tool", Width: 18}, {Label: "Pattern"}},
		Description: "Later matching rules take precedence. Plan uses read-only tools. Act follows rules and approval prompts. This view does not change permissions.",
		Actions: func() []table.Action {
			return []table.Action{{ID: "details", Label: "Details", Key: "enter", Enabled: view.Selected() != ""}, {ID: "close", Label: "Close", Key: "esc", Enabled: true}}
		},
		Activate: func(action, id string) tea.Cmd {
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
			m.chrome.Dialog.OnClose = func() {
				m.pendingCommands = append(m.pendingCommands, func() tea.Msg { return permissionTableMsg{view} })
			}
			return nil
		}}
}

type permissionTableMsg struct{ view *table.Model }
