package workflow

import (
	"context"
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
)

// Model owns one workflow screen and its background operations.
// All mutable screen state is confined to the Bubble Tea update loop.
type Model struct {
	dialogs                  screen.Host
	presentation             func() screen.Surface
	config                   Config
	ctx                      context.Context
	shutdown                 context.CancelFunc
	commands                 []tea.Cmd
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
	editor                   *Editor
	graph                    graph.Model
	registry                 *workflow.Registry
	notice                   string
	noticeLevel              pane.NoticeLevel
	offset                   int
	dragging                 bool
	last                     graph.Point
	cancel                   context.CancelFunc
	events                   chan tea.Msg
	run                      *workflow.Run
	runID                    string
	schedules                []store.Schedule
	scheduleStatus           []service.ScheduleInspection
	workerLeaseHeld          bool
}

// setNotice replaces both text and severity; a later update must not inherit
// the previous failure's presentation.
func (m *Model) setNotice(level pane.NoticeLevel, text string) {
	m.notice, m.noticeLevel = text, level
}

type workflowAction struct{ action, value string }
type workflowResult struct {
	text            string
	err             error
	source, path    string
	action          string
	schedules       []store.Schedule
	scheduleStatus  []service.ScheduleInspection
	workerLeaseHeld bool
	scheduleID      string
}
type workflowPermission struct {
	tool, resource string
	args           json.RawMessage
	reply          chan permission.Effect
}
type workflowRunID string
type workflowPoll struct {
	id  string
	run *workflow.Run
	err error
}

func (m *Model) queueAction(action, value string) {
	m.commands = append(m.commands, func() tea.Msg { return workflowAction{action, value} })
}
func (m *Model) service(gate permission.Gate) (service.Service, error) {
	return app.NewWorkflow(m.config.WS, userdirs.Sub("workflows"), m.config.Policy, gate)
}
func (m *Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.ensureSelection()
	key := msg.String()
	if m.inspectorOpen && (key == "1" || key == "2" || key == "3") {
		m.setInspectorTab(int(key[0] - '1'))
		m.inspect = true
		return m, nil
	}
	if m.inspectorOpen && m.inspect {
		switch key {
		case "left":
			m.setInspectorTab((m.inspectorTab + 2) % 3)
			return m, nil
		case "right":
			m.setInspectorTab((m.inspectorTab + 1) % 3)
			return m, nil
		case "e", "enter":
			if m.inspectorTab == 2 {
				m.editCode()
			} else if m.inspectorTab == 0 && m.run == nil && !m.busy {
				m.edit()
			}
			return m, nil
		}
	}
	action := ""
	switch key {
	case "esc":
		if m.busy {
			action = "stop"
		} else if m.connectFrom != "" {
			m.connectFrom = ""
			m.setNotice(pane.NoticeInfo, "Connection cancelled")
		} else if m.inspectorOpen && m.inspect {
			m.inspectorOpen = false
			m.inspect = false
			m.thumb = scrollDrag{}
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
		if m.run != nil {
			m.inspectorOpen = true
			m.inspect = true
			m.setInspectorTab(1)
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
		m.overview = !m.overview
	case "l":
		action = "layout"
	case "ctrl+z":
		action = "undo"
	case "ctrl+y":
		action = "redo"
	case "tab":
		if m.inspectorOpen {
			m.inspect = !m.inspect
		}
	case "up", "down", "left", "right":
		delta := 1
		if key == "up" || key == "left" {
			delta = -1
		}
		if m.inspect {
			m.offset = max(0, m.offset+delta)
		} else {
			m.portSelected = false
			m.offset = 0
			m.graph.SelectNext(delta)
			cw, ch := m.canvasSize()
			m.graph.Reveal(cw, ch)
		}
	case "shift+left":
		if m.positionsLocked {
			break
		}
		m.graph.Move(-2, 0)
	case "shift+right":
		if m.positionsLocked {
			break
		}
		m.graph.Move(2, 0)
	case "shift+up":
		if m.positionsLocked {
			break
		}
		m.graph.Move(0, -1)
	case "shift+down":
		if m.positionsLocked {
			break
		}
		m.graph.Move(0, 1)
	case "alt+left":
		m.graph.Pan.X -= 8
	case "alt+right":
		m.graph.Pan.X += 8
	case "alt+up":
		m.graph.Pan.Y -= 4
	case "alt+down":
		m.graph.Pan.Y += 4
	case "pgup", "pgdown", "home", "end":
		m.page(key)
	case "f1":
		m.dialogs.OpenDialog(screen.Dialog{Title: "Workflow keys", Lines: strings.Split("Ctrl+O open · Ctrl+N new · Ctrl+S save\nA add · Enter edit/inspect · Delete delete\nC connect · B connections · E node behavior · J complete recipe JSON\nM recipe name · U inputs · O outputs\nArrows select · Shift+arrows move · G locks positions · Alt+arrows pan\nDrag background to pan · Shift+wheel horizontal\nClick output then input to connect · Ctrl+T inspector\nTab pane focus · 1/2/3 inspector tabs · Ctrl+J raw data\nInspector E edits properties/code · Esc closes focused inspector\nF compact overview · L auto-layout · Ctrl+Z undo · Ctrl+Y redo\nV validate · R run · H saved runs · P review\nD editable draft · I AI proposal\nEsc chat (draft retained) / stop active operation\nF9 main menu · Ctrl+Q quit", "\n")})
		return m, nil
	}
	if strings.HasPrefix(key, "shift+") {
		m.persistLayout()
	}
	if action != "" {
		if m.run != nil && (action == "ai" || action == "edit" || action == "add" || action == "link" || action == "connections" || action == "delete" || action == "recipe") {
			m.setNotice(pane.NoticeInfo, "Viewing a frozen run · D returns to the editable draft")
			return m, nil
		}
		return m, m.command(action)
	}
	return m, nil
}
func (m *Model) inspectorContent() []string {
	view := m.presentation()
	width := view.Layout(view.Panes(true).Side.Width, true).Body.Width
	var out []string
	for _, line := range m.detailRows()[1:] {
		out = append(out, strings.Split(ansi.Hardwrap(line, max(1, width-1), true), "\n")...)
	}
	return out
}

func (m *Model) footerActions() []screen.Action {
	edit := !m.busy && m.run == nil
	label := "Edit"
	if m.run != nil {
		label = "Inspect"
	}
	back := "Chat"
	if m.busy {
		back = "Stop"
	} else if m.connectFrom != "" {
		back = "Cancel"
	} else if m.inspectorOpen && m.inspect {
		back = "Close"
	}
	actions := []screen.Action{
		{Key: tea.KeyF1, Name: "F1", Label: "Help", Short: "Help", Enabled: true},
		{Key: 'a', Name: "A", Label: "Add node", Short: "Add", Enabled: edit},
		{Key: tea.KeyEnter, Name: "Enter", Label: label, Short: label, Enabled: !m.busy},
		{Key: 'c', Name: "C", Label: "Connect", Short: "Link", Enabled: edit},
		{Key: 'r', Name: "R", Label: "Run", Short: "Run", Enabled: edit},
		{Key: 't', Mod: tea.ModCtrl, Name: "Ctrl+T", Label: "Inspector", Short: "Inspect", Enabled: true},
		{Key: 's', Mod: tea.ModCtrl, Name: "Ctrl+S", Label: "Save", Short: "Save", Enabled: edit},
		{Key: tea.KeyEscape, Name: "Esc", Label: back, Short: back, Enabled: true},
	}
	if m.presentation().Area.Width < 80 {
		return []screen.Action{actions[0], actions[1], actions[5], actions[7]}
	}
	return actions
}

// NewScreen constructs an inactive screen and takes ownership of cfg.Draft.
// Call Close when the terminal launch ends to cancel its operations.
func NewScreen(ctx context.Context, cfg Config, dialogs screen.Host, presentation func() screen.Surface) (*Model, error) {
	r, err := app.WorkflowRegistry(cfg.WS, userdirs.Sub("workflows"), nil, nil)
	if err != nil {
		return nil, err
	}
	draft := cfg.Draft
	if draft == nil {
		draft = New()
	}
	ctx, cancel := context.WithCancel(ctx)
	return &Model{
		dialogs: dialogs, presentation: presentation, config: cfg,
		ctx: ctx, shutdown: cancel, editor: draft, registry: r,
		events: make(chan tea.Msg, 16),
	}, nil
}

// Close cancels screen operations and releases its launch context.
func (m *Model) Close() {
	if m.cancel != nil {
		m.cancel()
	}
	m.shutdown()
}
func (m *Model) Init() tea.Cmd  { return nil }
func (m *Model) View() tea.View { return tea.NewView(m.content()) }
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.ctx.Err() != nil {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	case workflowProposal:
		m.acceptProposal(msg)
	case workflowAction:
		return m, m.action(msg)
	case workflowPermission:
		return m, m.acceptPermission(msg)
	case workflowResult:
		return m, m.acceptResult(msg)
	case workflowRunID:
		m.runID = string(msg)
		return m, tea.Batch(m.wait(), m.poll(string(msg), false))
	case workflowPoll:
		return m, m.acceptPoll(msg)
	}
	return m, nil
}
