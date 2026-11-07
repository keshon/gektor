package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/service"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	editor "github.com/keshon/gektor/internal/tui/workflow"
	"strings"
)

type workflowWorkspace struct {
	thumb                    scrollDrag
	active, busy, inspect    bool
	overview                 bool
	rawDetails               bool
	inspectorTab             int
	portFocus                string
	portOutput, portSelected bool
	positionsLocked          bool
	inspectorOpen, panning   bool
	connectFrom              string
	connectPort              string
	editor                   *editor.Editor
	graph                    graph.Model
	registry                 *automation.Registry
	notice                   string
	noticeLevel              pane.NoticeLevel
	offset                   int
	dragging                 bool
	last                     graph.Point
	cancel                   context.CancelFunc
	events                   chan tea.Msg
	run                      *automation.Run
	runID                    string
	schedules                []store.Schedule
}

// setNotice replaces both text and severity; a later update must not inherit
// the previous failure's presentation.
func (w *workflowWorkspace) setNotice(level pane.NoticeLevel, text string) {
	w.notice, w.noticeLevel = text, level
}

type workflowAction struct{ action, value string }
type workflowResult struct {
	text         string
	err          error
	source, path string
	action       string
	schedules    []store.Schedule
	scheduleID   string
}
type workflowPermission struct {
	tool, resource string
	args           json.RawMessage
	reply          chan permission.Effect
}
type workflowRunID string
type workflowPoll struct {
	id  string
	run *automation.Run
	err error
}

func (m *model) workflowActive() bool { return m.workflows != nil && m.workflows.active }
func (m *model) openWorkflows() {
	if m.state != stDone {
		m.openDialog("Workflows", []string{"Finish or stop the agent turn before opening the editor."})
		return
	}
	if m.workflows == nil {
		if m.baseEnv.WS == nil {
			m.openDialog("Workflows", []string{"Select a workspace first."})
			return
		}
		r, err := app.AutomationRegistry(m.baseEnv.WS, gektor.Sub("workflows"), nil, nil)
		if err != nil {
			m.openDialog("Workflows", []string{err.Error()})
			return
		}
		m.workflows = &workflowWorkspace{editor: editor.New(), registry: r, events: make(chan tea.Msg, 16)}
	}
	m.workflows.active = true
	m.inspector = toolInspector{}
	m.fitBottom()
}
func (m *model) queueWorkflow(action, value string) {
	m.pendingCommands = append(m.pendingCommands, func() tea.Msg { return workflowAction{action, value} })
}
func (m *model) workflowService(gate permission.Gate) (service.Service, error) {
	return app.NewAutomation(m.baseEnv.WS, gektor.Sub("workflows"), m.baseEnv.Policy, gate)
}
func (m *model) workflowKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	w := m.workflows
	m.workflowEnsureSelection()
	key := msg.String()
	if w.inspectorOpen && (key == "1" || key == "2" || key == "3") {
		m.workflowSetInspectorTab(int(key[0] - '1'))
		w.inspect = true
		return m, nil
	}
	if w.inspectorOpen && w.inspect {
		switch key {
		case "left":
			m.workflowSetInspectorTab((w.inspectorTab + 2) % 3)
			return m, nil
		case "right":
			m.workflowSetInspectorTab((w.inspectorTab + 1) % 3)
			return m, nil
		case "e", "enter":
			if w.inspectorTab == 2 {
				m.workflowEditCode()
			} else if w.inspectorTab == 0 && w.run == nil && !w.busy {
				m.workflowEdit()
			}
			return m, nil
		}
	}
	action := ""
	switch key {
	case "esc":
		if w.busy {
			action = "stop"
		} else if w.connectFrom != "" {
			w.connectFrom = ""
			w.setNotice(pane.NoticeInfo, "Connection cancelled")
		} else if w.inspectorOpen && w.inspect {
			w.inspectorOpen = false
			w.inspect = false
			w.thumb = scrollDrag{}
		} else {
			action = "chat"
		}
	case "ctrl+j":
		action = "raw-details"
	case "ctrl+t":
		action = "inspector"
	case "ctrl+o":
		action = "open"
	case "ctrl+n":
		action = "new"
	case "ctrl+s":
		action = "save"
	case "a":
		action = "add"
	case "enter":
		if w.run != nil {
			w.inspectorOpen = true
			w.inspect = true
			m.workflowSetInspectorTab(1)
			return m, nil
		}
		action = "edit"
	case "g":
		action = "lock-positions"
	case "c":
		action = "link"
	case "b":
		action = "connections"
	case "delete":
		action = "delete"
	case "e":
		action = "behavior"
	case "i":
		action = "ai"
	case "j":
		action = "recipe"
	case "m":
		action = "name"
	case "u":
		action = "inputs"
	case "o":
		action = "outputs"
	case "v":
		action = "validate"
	case "r":
		action = "run"
	case "h":
		action = "runs"
	case "p":
		action = "review"
	case "d":
		action = "draft"
	case "f":
		w.overview = !w.overview
	case "l":
		action = "layout"
	case "ctrl+z":
		action = "undo"
	case "ctrl+y":
		action = "redo"
	case "tab":
		if w.inspectorOpen {
			w.inspect = !w.inspect
		}
	case "up", "down", "left", "right":
		delta := 1
		if key == "up" || key == "left" {
			delta = -1
		}
		if w.inspect {
			w.offset = max(0, w.offset+delta)
		} else {
			w.portSelected = false
			w.offset = 0
			w.graph.SelectNext(delta)
			cw, ch := m.workflowCanvasSize()
			w.graph.Reveal(cw, ch)
		}
	case "shift+left":
		if w.positionsLocked {
			break
		}
		w.graph.Move(-2, 0)
	case "shift+right":
		if w.positionsLocked {
			break
		}
		w.graph.Move(2, 0)
	case "shift+up":
		if w.positionsLocked {
			break
		}
		w.graph.Move(0, -1)
	case "shift+down":
		if w.positionsLocked {
			break
		}
		w.graph.Move(0, 1)
	case "alt+left":
		w.graph.Pan.X -= 8
	case "alt+right":
		w.graph.Pan.X += 8
	case "alt+up":
		w.graph.Pan.Y -= 4
	case "alt+down":
		w.graph.Pan.Y += 4
	case "pgup", "pgdown", "home", "end":
		m.workflowPage(key)
	case "f1":
		m.openDialog("Workflow keys", strings.Split("Ctrl+O open · Ctrl+N new · Ctrl+S save\nA add · Enter edit/inspect · Delete delete\nC connect · B connections · E node behavior · J complete recipe JSON\nM recipe name · U inputs · O outputs\nArrows select · Shift+arrows move · G locks positions · Alt+arrows pan\nDrag background to pan · Shift+wheel horizontal\nClick output then input to connect · Ctrl+T inspector\nTab pane focus · 1/2/3 inspector tabs · Ctrl+J raw data\nInspector E edits properties/code · Esc closes focused inspector\nF compact overview · L auto-layout · Ctrl+Z undo · Ctrl+Y redo\nV validate · R run · H saved runs · P review\nD editable draft · I AI proposal\nEsc chat (draft retained) / stop active operation\nF9 main menu · Ctrl+Q quit", "\n"))
		return m, nil
	case "f9":
		return m.footerKey(msg)
	case "f10":
		return m.requestQuit()
	}
	if strings.HasPrefix(key, "shift+") {
		m.persistWorkflowLayout()
	}
	if action != "" {
		if w.run != nil && (action == "ai" || action == "edit" || action == "add" || action == "link" || action == "connections" || action == "delete" || action == "recipe") {
			w.setNotice(pane.NoticeInfo, "Viewing a frozen run · D returns to the editable draft")
			return m, nil
		}
		return m, m.workflowCommand(action)
	}
	return m, nil
}
func (m *model) workflowInspectorContent() []string {
	width := m.panelGeometry(m.screenPanes(true).Side.Width, 1, true, false).Body.Width
	var out []string
	for _, line := range m.workflowDetailRows()[1:] {
		out = append(out, strings.Split(ansi.Hardwrap(line, max(1, width-1), true), "\n")...)
	}
	return out
}

func (m *model) workflowFooterActions() []footerAction {
	w := m.workflows
	edit := !w.busy && w.run == nil
	label := "Edit"
	if w.run != nil {
		label = "Inspect"
	}
	back := "Chat"
	if w.busy {
		back = "Stop"
	} else if w.connectFrom != "" {
		back = "Cancel"
	} else if w.inspectorOpen && w.inspect {
		back = "Close"
	}
	actions := []footerAction{{tea.KeyF1, 0, "F1", "Help", "Help", true}, {'a', 0, "A", "Add node", "Add", edit}, {tea.KeyEnter, 0, "Enter", label, label, !w.busy}, {'c', 0, "C", "Connect", "Link", edit}, {'r', 0, "R", "Run", "Run", edit}, {'t', tea.ModCtrl, "Ctrl+T", "Inspector", "Inspect", true}, {'s', tea.ModCtrl, "Ctrl+S", "Save", "Save", edit}, {tea.KeyEscape, 0, "Esc", back, back, true}}
	if m.termW < 80 {
		return []footerAction{actions[0], actions[1], actions[5], actions[7]}
	}
	return actions
}
