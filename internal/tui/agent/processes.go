package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	toolbackground "github.com/keshon/gektor/internal/tools/background"
	"github.com/keshon/gektor/internal/tui/components/table"
	"github.com/keshon/gektor/internal/tui/screen"
)

type processListMsg struct{ view *table.Model }
type processStopMsg struct {
	id   string
	view *table.Model
}
type processStoppedMsg struct {
	dialog *screen.DialogState
	view   *table.Model
	id     string
	err    error
}

func processStatus(p toolbackground.Process) string {
	if p.Exited {
		if p.Stopped {
			return "Stopped"
		}
		if p.Error != "" {
			return "Failed"
		}
		return "Exited"
	}
	if p.PID == 0 {
		return "Starting"
	}
	return "Running"
}

func (m *Model) openProcesses(view *table.Model) {
	if view == nil {
		view = m.chrome.NewTable()
	}
	m.chrome.Open("Background processes", nil)
	d := &screen.TableState{Model: view,
		Columns:      []table.Column{{Label: "ID", Width: 6}, {Label: "Status", Width: 10}, {Label: "PID", Width: 7, HideNarrow: true}, {Label: "Session", Width: 10, HideNarrow: true}, {Label: "Command"}},
		Description:  "This GEKTOR launch only. Closing GEKTOR or applying settings stops these processes. Details include workspace and captured output.",
		EmptyMessage: "No managed background processes in this launch.",
	}

	m.chrome.Dialog.Table = d
	m.processDialog = m.chrome.Dialog
	m.refreshProcesses(view)
}

func (m *Model) returnToProcesses(view *table.Model) {
	m.pendingCommands = append(m.pendingCommands, func() tea.Msg { return processListMsg{view} })
}

func (m *Model) inspectProcess(id string, view *table.Model) {
	p, output, err := m.baseEnv.Procs.Inspect(id)
	if err != nil {
		m.chrome.Open("Background process", []string{err.Error()})
	} else {
		args, _ := json.Marshal(p.Args)
		command, _ := json.Marshal(p.Command)
		lines := []string{"ID: " + p.ID, "Status: " + processStatus(p), fmt.Sprintf("PID: %d", p.PID), "Command: " + string(command), "Arguments: " + string(args), "Workspace: " + p.Directory, "Session: " + p.Session, "Started: " + p.Started.Local().Format("2006-01-02 15:04:05")}
		if p.Error != "" {
			lines = append(lines, "Exit: "+p.Error)
		}
		lines = append(lines, "", "Captured output (snapshot; reopen to refresh):", ansi.Strip(output))
		m.chrome.Open("Background process / "+id, strings.Split(strings.Join(lines, "\n"), "\n"))
	}
	m.rememberReturn(dialogReturn{processes: view})
}

func (m *Model) confirmStopProcess(id string, view *table.Model) {
	p, _, err := m.baseEnv.Procs.Inspect(id)
	if err != nil || p.Exited || p.PID == 0 {
		m.openProcesses(view)
		return
	}
	m.openForm(screen.Form{
		Title:      "Stop background process / " + id,
		Hint:       fmt.Sprintf("Stop this process and its children?\nPID: %d\nWorkspace: %s\n%s", p.PID, p.Directory, ansi.Strip(p.Command)),
		ApplyLabel: "Stop",
		Fields:     nil,
	}, func(_ map[string]string) error {
		delete(m.returnDialogs, m.chrome.Dialog)
		m.pendingCommands = append(m.pendingCommands, func() tea.Msg { return processStopMsg{id, view} })
		return nil
	})
	m.rememberReturn(dialogReturn{processes: view})
	m.chrome.Dialog.Form.Destructive = true
	m.chrome.FocusForm(1)
}

func (m *Model) stopProcess(msg processStopMsg) tea.Cmd {
	if m.chrome.Dialog != nil || m.state == stPermission || m.state == stAsk {
		return nil
	}
	m.chrome.Open("Stopping background process / "+msg.id, []string{"Waiting for the process and its children to exit…"})
	dialog, procs := m.chrome.Dialog, m.baseEnv.Procs
	return func() tea.Msg {
		return processStoppedMsg{dialog: dialog, view: msg.view, id: msg.id, err: procs.Stop(msg.id)}
	}
}

func (m *Model) refreshProcesses(view *table.Model) {
	statuses := map[string]toolbackground.Process{}

	rows := []table.Row{}
	for _, p := range m.baseEnv.Procs.List() {
		owner := "Unassigned"
		if p.Session != "" {
			owner = filepath.Base(filepath.Dir(p.Session))
		}
		pid := "—"
		if p.PID > 0 {
			pid = strconv.Itoa(p.PID)
		}
		rows = append(rows, table.Row{ID: p.ID, Cells: []string{p.ID, processStatus(p), pid, owner, ansi.Strip(p.Command)}})
		statuses[p.ID] = p
	}
	m.chrome.RefreshTable(rows)

	m.chrome.Dialog.Table.Actions = m.processActions(view, statuses)
}

func (m *Model) processActions(view *table.Model, statuses map[string]toolbackground.Process) []table.Action {
	p, exists := statuses[view.Selected()]
	return []table.Action{
		{ID: "details", Label: "Output", Key: "enter", Enabled: exists},
		{ID: "stop", Label: "Stop…", Key: "s", Enabled: exists && !p.Exited && p.PID > 0, Destructive: true},
		{ID: "refresh", Label: "Refresh", Key: "r", Enabled: true},
		{ID: "close", Label: "Close", Key: "esc", Enabled: true},
	}
}

func (m *Model) processAction(view *table.Model, action, id string) tea.Cmd {
	switch action {
	case "refresh":
		m.refreshProcesses(view)
	case "details":
		m.inspectProcess(id, view)
	case "stop":
		m.confirmStopProcess(id, view)
	}
	return nil
}
