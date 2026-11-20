package agent

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/pkg/tui/components/table"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func (m *Model) openPermissions() {
	m.openPermissionTable(m.chrome.NewTable())
}

func (m *Model) openPermissionTable(view screen.TableID) {
	rows := make([]table.Row, 0, len(m.policyDetails))
	for i, rule := range m.policyDetails {
		rows = append(rows, table.Row{ID: strconv.Itoa(i), Cells: []string{strconv.Itoa(i + 1), rule.Effect.String(), rule.Tool, rule.Pattern}})
	}

	m.chrome.PresentDialog(screen.Dialog{Title: "Permissions", Table: &screen.Table{State: view, Rows: rows,
		Columns:     []table.Column{{Label: "Order", Width: 5}, {Label: "Decision", Width: 8}, {Label: "Tool", Width: 18}, {Label: "Pattern"}},
		Description: "Later matching rules take precedence. Plan uses read-only tools. Act follows rules and approval prompts. This view does not change permissions.",
		Actions: []table.Action{
			{ID: "details", Label: "Details", Key: "enter", Enabled: len(rows) > 0},
			{ID: "close", Label: "Close", Key: "esc", Enabled: true},
		},
	}})
	m.permissionTable = view
	m.permissionDialog = m.chrome.DialogID()
}

type permissionTableMsg struct{ view screen.TableID }

func (m *Model) permissionAction(view screen.TableID, action, id string) tea.Cmd {
	if action != "details" {
		return nil
	}
	i, err := strconv.Atoi(id)
	if err != nil || i < 0 || i >= len(m.policyDetails) {
		return nil
	}
	rule := m.policyDetails[i]
	m.chrome.Close()
	m.chrome.Open("Permission rule "+strconv.Itoa(i+1), []string{"Decision: " + rule.Effect.String(), "Tool: " + rule.Tool, "Pattern: " + rule.Pattern, "", "Later matching rules take precedence."})
	m.rememberReturn(dialogReturn{permissions: view})
	return nil
}
