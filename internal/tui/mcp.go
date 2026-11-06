package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/mcpmanager"
)

type mcpActionMsg struct{ action, id string }
type mcpDoneMsg struct {
	id    string
	count int
	err   error
}

func (m *model) queueMCP(action, id string) {
	m.pendingCommands = append(m.pendingCommands, func() tea.Msg { return mcpActionMsg{action, id} })
}
func (m *model) openMCP() {
	if m.mcp == nil {
		m.openDialog("MCP servers", []string{"MCP management is unavailable in this launch."})
		return
	}
	if m.mcpBusy {
		m.openDialog("MCP servers", []string{"Connecting…"})
		return
	}
	options := []string{}
	for _, s := range m.mcp.List() {
		state := "disabled"
		if s.Enabled {
			state = "on demand"
		}
		if s.Enabled && s.Startup {
			state = "at startup"
		}
		if s.Connected {
			state = fmt.Sprintf("connected, %d tools", s.Tools)
		}
		if s.State == "connecting" {
			state = "connecting"
		}
		if s.Error != "" {
			state = "failed"
		}
		options = append(options, s.ID+" · "+state)
	}
	selected := ""
	if len(options) > 0 {
		selected = options[0]
	}
	action := "Add"
	if len(options) > 0 {
		action = "Details"
	}
	m.openForm("MCP servers", "Saved connections · changes are available between agent turns.", "Continue", []formField{{Key: "mcp-server", Label: "Server", Value: selected}, {Key: "mcp-action", Label: "Action", Value: action}}, func(v map[string]string) error {
		id, _, _ := strings.Cut(v["mcp-server"], " · ")
		a := strings.ToLower(v["mcp-action"])
		if a != "add" && a != "import" && id == "" {
			return fmt.Errorf("select a server")
		}
		m.queueMCP(a, id)
		return nil
	})
	m.dialog.form.Choices = map[string][]string{"mcp-server": options, "mcp-action": {"Details", "Connect", "Disconnect", "Reconnect", "Enable", "Disable", "Edit", "Delete", "Add", "Import"}}
}
func (m *model) handleMCPAction(a mcpActionMsg) tea.Cmd {
	if m.mcp == nil {
		return nil
	}
	if m.mcpBusy {
		m.openDialog("MCP servers", []string{"Wait for the connection attempt to finish."})
		return nil
	}
	if a.action != "details" && (m.state != stDone || m.evaluation != nil) {
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
			m.queueMCP("list", "")
			return nil
		})
		m.dialog.form.Choices = map[string][]string{"enabled": {"on", "off"}, "loading": {"on demand", "at startup"}}
	case "import":
		m.openForm("Import MCP servers", "Select a JSON file containing mcpServers. Credentials are stored separately.", "Import", []formField{{Key: "path", Label: "Configuration file"}, {Key: "replace", Label: "Replace existing servers", Value: "off"}}, func(v map[string]string) error {
			raw, e := os.ReadFile(strings.Trim(v["path"], "\""))
			if e != nil {
				return e
			}
			if _, e = m.mcp.Import(raw, v["replace"] == "on"); e != nil {
				return e
			}
			m.queueMCP("list", "")
			return nil
		})
		m.dialog.form.Choices = map[string][]string{"replace": {"off", "on"}}
	case "delete":
		m.openForm("Delete MCP / "+a.id, "Remove this saved server and disconnect it.", "Delete", nil, func(map[string]string) error {
			if e := m.mcp.Delete(a.id); e != nil {
				return e
			}
			m.queueMCP("list", "")
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
				raw, _ := json.MarshalIndent(s, "", "  ")
				m.openDialog("MCP / "+a.id, strings.Split(string(raw), "\n"))
				break
			}
		}
	default:
		fail(fmt.Errorf("unknown MCP action %q", a.action))
	}
	return nil
}
