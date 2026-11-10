package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/mcpmanager"
	"github.com/keshon/gektor/internal/tui/components/table"
)

type mcpActionMsg struct{ action, id string }
type mcpDoneMsg struct {
	id    string
	count int
	err   error
}

func (m *Model) queueMCP(action, id string) {
	m.pendingCommands = append(m.pendingCommands, func() tea.Msg { return mcpActionMsg{action, id} })
}
func (m *Model) openMCP() {
	if m.mcp == nil {
		m.openDialog("MCP servers", []string{"MCP management is unavailable in this launch."})
		return
	}
	if m.mcpBusy {
		m.openDialog("MCP servers", []string{"Connecting…"})
		return
	}
	view := m.newTable()
	if m.mcpTable != nil {
		view = m.mcpTable.model
	}
	statuses := map[string]mcpmanager.Status{}
	rows := []table.Row{}
	for _, server := range m.mcp.List() {
		statuses[server.ID] = server
		state := "Disabled"
		if server.Enabled {
			state = "On demand"
		}
		if server.Enabled && server.Startup {
			state = "At startup"
		}
		if server.Connected {
			state = "Connected"
		}
		if server.State == "connecting" {
			state = "Connecting"
		}
		if server.Error != "" {
			state = "Failed"
		}
		rows = append(rows, table.Row{ID: server.ID, Cells: []string{server.ID, state, fmt.Sprint(server.Tools), server.Description}})
	}
	view.SetRows(rows)
	m.openDialog("MCP servers", nil)
	d := &tableDialog{model: view,
		columns:     []table.Column{{Label: "Server", Width: 14}, {Label: "Status", Width: 15}, {Label: "Tools", Width: 5, HideNarrow: true}, {Label: "Description", HideNarrow: true}},
		description: "More: Add, Paste JSON, Import file, Enable/Disable, Delete.",
	}
	d.actions = func() []table.Action {
		selected := view.Selected()
		status, exists := statuses[selected]
		idle := m.state == stDone && m.evaluation == nil && !m.mcpBusy
		connection, label := "connect", "Connect"
		if status.Connected {
			connection, label = "disconnect", "Disconnect"
		}
		toggle, toggleLabel := "enable", "Enable"
		if status.Enabled {
			toggle, toggleLabel = "disable", "Disable"
		}
		return []table.Action{
			{ID: "details", Label: "Details", Key: "enter", Enabled: exists},
			{ID: connection, Label: label, Key: "c", Enabled: exists && idle && (status.Enabled || status.Connected)},
			{ID: "edit", Label: "Edit", Key: "e", Enabled: exists && idle},
			{ID: toggle, Label: toggleLabel, Key: "t", Enabled: exists && idle, Overflow: true},
			{ID: "delete", Label: "Delete", Key: "delete", Enabled: exists && idle, Destructive: true, Overflow: true},
			{ID: "add", Label: "Add", Key: "a", Enabled: idle, Overflow: true},
			{ID: "import-json", Label: "Paste JSON", Key: "p", Enabled: idle, Overflow: true},
			{ID: "import", Label: "Import file", Key: "i", Enabled: idle, Overflow: true},
			{ID: "close", Label: "Close", Key: "esc", Enabled: true},
		}
	}
	d.activate = func(action, id string) tea.Cmd {
		m.closeDialog()
		return m.handleMCPAction(mcpActionMsg{action, id})
	}
	m.dialog.table, m.mcpTable = d, d
}

func (m *Model) handleMCPAction(a mcpActionMsg) tea.Cmd {
	if m.mcp == nil {
		return nil
	}
	if a.action == "list" && (m.dialog != nil || m.state == stPermission || m.state == stAsk) {
		return nil
	}
	if m.mcpBusy {
		m.openDialog("MCP servers", []string{"Wait for the connection attempt to finish."})
		return nil
	}
	if a.action != "details" && a.action != "list" && (m.state != stDone || m.evaluation != nil) {
		m.openDialog("MCP servers", []string{"Finish or stop the agent turn before changing connections."})
		return nil
	}
	fail := func(e error) { m.openDialog("MCP servers", []string{e.Error()}) }
	switch a.action {
	case "list":
		m.openMCP()
	case "add", "edit":
		s := mcpmanager.Server{Enabled: true}
		if a.action == "edit" {
			var e error
			s, e = m.mcp.Get(a.id)
			if e != nil {
				fail(e)
				return nil
			}
		}
		command, _ := json.Marshal(s.Command)
		refs, _ := json.Marshal(s.EnvRefs)
		enabled, loading := "on", "on demand"
		if !s.Enabled {
			enabled = "off"
		}
		if s.Startup {
			loading = "at startup"
		}
		m.openForm("MCP / "+a.action, "Use a URL or a command array. Import JSON to add HTTP credentials.", "Save", []formField{{Key: "id", Label: "ID", Value: s.ID}, {Key: "description", Label: "Description", Value: s.Description}, {Key: "url", Label: "HTTP endpoint", Value: s.URL}, {Key: "command", Label: "Command and arguments (JSON array)", Value: string(command)}, {Key: "envrefs", Label: "Environment references (JSON object)", Value: string(refs)}, {Key: "enabled", Label: "Enabled", Value: enabled}, {Key: "loading", Label: "Loading", Value: loading}}, func(v map[string]string) error {
			if a.action == "edit" && v["id"] != s.ID {
				return fmt.Errorf("server IDs cannot be renamed; add a new server")
			}
			if a.action == "add" {
				if _, e := m.mcp.Get(v["id"]); e == nil {
					return fmt.Errorf("server already exists")
				}
			}
			next := s
			next.ID = v["id"]
			next.Description = v["description"]
			next.URL = v["url"]
			next.Enabled = v["enabled"] == "on"
			next.Startup = v["loading"] == "at startup"
			if json.Unmarshal([]byte(v["command"]), &next.Command) != nil {
				return fmt.Errorf("command must be a JSON array")
			}
			if json.Unmarshal([]byte(v["envrefs"]), &next.EnvRefs) != nil {
				return fmt.Errorf("environment references must be a JSON object")
			}
			if next.URL != s.URL && len(s.HeaderRefs) > 0 {
				return fmt.Errorf("authenticated endpoint changes require importing a new server")
			}
			if e := m.mcp.Save(next); e != nil {
				return e
			}
			return nil
		})
		m.dialog.form.Choices = map[string][]string{"enabled": {"on", "off"}, "loading": {"on demand", "at startup"}}
	case "import", "import-json":
		title := "Import MCP file"
		hint := "Select a JSON file containing mcpServers. Credentials are stored separately."
		field := formField{Key: "path", Label: "Configuration file"}
		if a.action == "import-json" {
			title = "Paste MCP configuration"
			hint = "Paste the JSON object containing mcpServers. Credentials are stored separately."
			field = formField{Key: "json", Label: "JSON configuration", Multiline: true}
		}
		m.openForm(title, hint, "Import", []formField{field, {Key: "replace", Label: "Replace existing servers", Value: "off"}}, func(v map[string]string) error {
			raw := []byte(v["json"])
			if a.action == "import" {
				var e error
				raw, e = os.ReadFile(strings.Trim(v["path"], "\""))
				if e != nil {
					return e
				}
			}
			if _, e := m.mcp.Import(raw, v["replace"] == "on"); e != nil {
				return e
			}
			return nil
		})
		m.dialog.form.Choices = map[string][]string{"replace": {"off", "on"}}
	case "delete":
		m.openForm("Delete MCP / "+a.id, "Remove this saved server and disconnect it.", "Delete", nil, func(map[string]string) error {
			if e := m.mcp.Delete(a.id); e != nil {
				return e
			}
			return nil
		})
		m.dialog.form.Destructive = true
		m.focusForm(1)
	case "enable", "disable":
		s, e := m.mcp.Get(a.id)
		if e != nil {
			fail(e)
			return nil
		}
		s.Enabled = a.action == "enable"
		if e = m.mcp.Save(s); e != nil {
			fail(e)
		} else {
			m.openMCP()
		}
	case "disconnect":
		if e := m.mcp.Disconnect(a.id); e != nil {
			fail(e)
		} else {
			m.openMCP()
		}
	case "connect", "reconnect":
		if a.action == "reconnect" {
			if e := m.mcp.Disconnect(a.id); e != nil {
				fail(e)
				return nil
			}
		}
		m.mcpBusy = true
		m.openDialog("MCP / "+a.id, []string{"Connecting and discovering tools…"})
		manager, base, id := m.mcp, m.base, a.id
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(base, 20*time.Second)
			defer cancel()
			n, e := manager.Connect(ctx, id)
			return mcpDoneMsg{id, n, e}
		}
	case "details":
		for _, s := range m.mcp.List() {
			if s.ID == a.id {
				lines := []string{"Server: " + s.ID, "Description: " + s.Description, "Connection: " + s.State,
					fmt.Sprintf("Enabled: %t", s.Enabled), fmt.Sprintf("Tools: %d", s.Tools)}
				if s.URL != "" {
					lines = append(lines, "Endpoint: "+s.URL)
				}
				if len(s.Command) > 0 {
					lines = append(lines, "Command: "+strings.Join(s.Command, " "))
				}
				if s.Error != "" {
					lines = append(lines, "", "Connection error", s.Error)
				}
				if len(s.ToolNames) > 0 {
					lines = append(lines, "", "Available tools")
					lines = append(lines, s.ToolNames...)
				}
				m.openDialog("MCP / "+a.id, lines)
				break
			}
		}
	default:
		fail(fmt.Errorf("unknown MCP action %q", a.action))
	}
	if a.action != "list" && m.dialog != nil && m.dialog.table == nil && !m.mcpBusy {
		m.dialog.onClose = func() { m.queueMCP("list", a.id) }
	}
	return nil
}
