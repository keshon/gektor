package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"context"
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/service"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/schemaform"
	"github.com/keshon/gektor/internal/tui/settings"
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
	offset                   int
	dragging                 bool
	last                     graph.Point
	cancel                   context.CancelFunc
	events                   chan tea.Msg
	run                      *automation.Run
	runID                    string
	schedules                []store.Schedule
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
		r, err := app.AutomationRegistry(m.baseEnv.WS, gektor.Sub("workflows"), nil)
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
func (m *model) workflowCommand(action string) tea.Cmd {
	if !m.workflowActive() {
		m.openWorkflows()
	}
	if !m.workflowActive() {
		return nil
	}
	w := m.workflows
	if action == "inspector" {
		w.inspectorOpen = !w.inspectorOpen
		w.inspect = w.inspectorOpen
		w.thumb = scrollDrag{}
		return nil
	}
	if action == "lock-positions" {
		w.positionsLocked = !w.positionsLocked
		w.dragging = false
		m.persistWorkflowLayout()
		return nil
	}
	if action == "raw-details" {
		w.rawDetails = !w.rawDetails
		w.offset = 0
		return nil
	}
	if action == "overview" {
		w.overview = !w.overview
		return nil
	}
	if action == "" {
		return nil
	}

	if w.busy && action != "stop" {
		w.notice = "Operation in progress · Esc stops it"
		return nil
	}
	if w.run != nil && (action == "edit" || action == "add" || action == "link" || action == "connections" || action == "delete" || action == "recipe" || action == "name" || action == "inputs" || action == "outputs" || action == "ai" || action == "behavior" || action == "undo" || action == "redo" || action == "save" || action == "run" || action == "validate") {
		w.notice = "Frozen run · D returns to the editable draft"
		return nil
	}
	if action == "chat" {
		w.active = false
		return nil
	}
	if m.workflowSelectedSchedule() != nil && (action == "behavior" || action == "connections" || action == "link") {
		w.notice = "Schedule starts the whole recipe. Enter edits the trigger; connections are automatic."
		return nil
	}
	fail := func(err error) { m.openDialog("Workflow", []string{err.Error()}) }
	switch action {
	case "schedule":
		m.workflowSchedule()
	case "schedule-add":
		m.workflowScheduleStart(true)
	case "schedule-refresh":
		return m.workflowScheduleRefresh()
	case "help":
		_, cmd := m.workflowKey(tea.KeyPressMsg{Code: tea.KeyF1})
		return cmd
	case "setup":
		if w.run != nil {
			return nil
		}
		m.openForm("Edit workflow", "Choose what to change. AI proposals are reviewed before applying.", "Open", []formField{{Key: "section", Label: "Section", Value: "Selected step arguments"}}, func(v map[string]string) error {
			actions := map[string]string{"Selected step arguments": "edit", "AI change": "ai", "Name": "name", "Inputs": "inputs", "Outputs": "outputs", "Complete JSON": "recipe"}
			action, ok := actions[v["section"]]
			if !ok {
				return fmt.Errorf("choose a section")
			}
			m.queueWorkflow(action, "")
			return nil
		})
		m.dialog.form.Choices = map[string][]string{"section": {"Selected step arguments", "AI change", "Name", "Inputs", "Outputs", "Complete JSON"}}
	case "name":
		if w.run == nil {
			m.workflowRecipeName()
		}
	case "inputs", "outputs":
		if w.run == nil {
			m.workflowParameters(action)
		}
	case "stop":
		if w.cancel != nil {
			w.cancel()
		}
		w.notice = "Stopping…"
	case "new", "open":
		fields := []formField{{Key: "path", Label: "Recipe path (relative to workspace)", Value: w.editor.Path}}
		if action == "new" {
			fields[0].Value = "recipe.json"
		}
		if w.editor.Dirty() {
			fields = append(fields, formField{Key: "discard", Label: "Unsaved changes: type discard to replace"})
		}
		m.openForm("Workflow / "+action, "Workspace: "+m.wsRoot, "Continue", fields, func(v map[string]string) error {
			if w.editor.Dirty() && v["discard"] != "discard" {
				return fmt.Errorf("save first, or type discard")
			}
			if strings.TrimSpace(v["path"]) == "" {
				return fmt.Errorf("enter a recipe path")
			}
			if _, err := m.baseEnv.WS.Relative(v["path"]); err != nil {
				return err
			}
			m.queueWorkflow(action+"-path", v["path"])
			return nil
		})
		if action == "open" {
			m.dialog.form.FileFields = map[string]settings.FilePicker{"path": {Root: m.wsRoot, Extensions: []string{".json"}}}
		}
	case "save":
		m.openForm("Save workflow", "Writes the recipe; canvas positions stay in your user settings.", "Save", []formField{{Key: "path", Label: "Recipe path", Value: w.editor.Path}}, func(v map[string]string) error {
			if strings.TrimSpace(v["path"]) == "" {
				return fmt.Errorf("enter a path")
			}
			m.queueWorkflow("save-path", v["path"])
			return nil
		})
	case "add":
		names := append(w.registry.Names(), "Schedule trigger")
		m.openForm("Add node", "Choose an operation or a Schedule trigger.", "Next", []formField{{Key: "type", Label: "Node type", Value: names[0]}}, func(v map[string]string) error {
			if v["type"] == "Schedule trigger" {
				m.queueWorkflow("schedule-add", "")
				return nil
			}
			if _, err := w.registry.Node(v["type"]); err != nil {
				return err
			}
			m.queueWorkflow("add-operation", v["type"])
			return nil
		})
		m.dialog.form.Choices = map[string][]string{"type": names}
	case "edit":
		m.workflowEdit()
	case "behavior":
		for i, step := range w.editor.Definition.Steps {
			if step.ID != w.graph.Selected {
				continue
			}
			original, _ := json.Marshal(step)
			schema := schemaform.Parse(json.RawMessage(`{"properties":{"review":{"type":"boolean","enum":[true,false]},"depends_on":{"type":"array"},"when":{},"for_each":{},"merge":{"type":"string","enum":["all","any"]},"retry":{"type":"object"}}}`))
			m.openForm(step.ID+" / behavior", "Conditions and references use JSON. Retries require a retry-safe operation.", "Apply", schema.Fields(original), func(v map[string]string) error {
				raw, err := schema.Values(original, v)
				if err != nil {
					return err
				}
				var changed automation.Step
				if err := json.Unmarshal(raw, &changed); err != nil {
					return err
				}
				d := w.editor.Clone()
				d.Steps[i] = changed
				w.editor.Replace(d)
				w.graph = m.workflowGraph(w.graph)
				return nil
			})
			m.dialog.form.Choices = schema.Choices()
			break
		}

	case "ai":
		m.openForm("AI workflow proposal", "The model proposes recipe edits only. You review before applying.", "Propose", []formField{{Key: "prompt", Label: "Describe the change", Multiline: true}}, func(v map[string]string) error {
			if strings.TrimSpace(v["prompt"]) == "" {
				return fmt.Errorf("describe a change")
			}
			m.queueWorkflow("ai-prompt", v["prompt"])
			return nil
		})
	case "recipe":
		m.openForm("Recipe JSON", "Inputs, outputs, conditions, iteration and retry are preserved here.", "Apply", []formField{{Key: "source", Label: "Recipe", Value: w.editor.Source(), Multiline: true}}, func(v map[string]string) error {
			d, err := editor.Decode([]byte(v["source"]))
			if err != nil {
				return err
			}
			w.editor.Replace(d)
			w.graph = m.workflowGraph(w.graph)
			return nil
		})
	case "link":
		names := []string{}
		for _, s := range w.editor.Definition.Steps {
			names = append(names, s.ID)
		}
		if len(names) < 2 {
			w.notice = "Add at least two nodes first"
			return nil
		}
		destination := names[0]
		if destination == w.graph.Selected {
			destination = names[1]
		}
		m.openForm("Connect nodes", "Choose the source and destination, then map their data.", "Next", []formField{{Key: "from", Label: "Source node", Value: w.graph.Selected}, {Key: "to", Label: "Destination node", Value: destination}}, func(v map[string]string) error {
			if v["from"] == v["to"] {
				return fmt.Errorf("choose different nodes")
			}
			payload, _ := json.Marshal([]string{v["from"], v["to"]})
			m.queueWorkflow("link-target", string(payload))
			return nil
		})
		m.dialog.form.Choices = map[string][]string{"from": names, "to": names}
	case "connections":
		m.workflowConnections()

	case "delete":
		if schedule := m.workflowSelectedSchedule(); schedule != nil {
			m.workflowDeleteSchedule(*schedule)
			return nil
		}
		id := w.graph.Selected
		m.openForm("Delete node", "References to this node must be repaired before validation succeeds.", "Delete", []formField{{Key: "id", Label: "Type node ID to delete"}}, func(v map[string]string) error {
			if id == "" || v["id"] != id {
				return fmt.Errorf("type %s", id)
			}
			d := w.editor.Clone()
			for i, s := range d.Steps {
				if s.ID == id {
					d.Steps = append(d.Steps[:i], d.Steps[i+1:]...)
					break
				}
			}
			w.editor.Replace(d)
			w.graph.Selected = ""
			w.graph = m.workflowGraph(w.graph)
			return nil
		})
	case "run-json", "validate-json":
		base := strings.TrimSuffix(action, "-json")
		m.workflowInputForm(workflowInvocation{Action: base, Draft: w.editor.Source(), Mode: "JSON"})
	case "validate", "run":
		m.workflowRunInputs(action)
	case "runs":
		s, err := store.OpenStore(gektor.Sub("workflows"), m.wsRoot)
		if err != nil {
			fail(err)
			return nil
		}
		defer s.Close()
		runs, err := s.Runs()
		if err != nil {
			fail(err)
			return nil
		}
		options := []string{}
		ids := map[string]string{}
		for _, r := range runs {
			label := cleanText(r.Name) + " / " + r.Status + " / " + r.ID
			options = append(options, label)
			ids[label] = r.ID
		}
		if len(options) == 0 {
			w.notice = "No runs in this workspace"
			return nil
		}
		m.openForm("Workflow runs", "Open an immutable execution record.", "Open", []formField{{Key: "run", Label: "Run", Value: options[len(options)-1]}}, func(v map[string]string) error {
			id, ok := ids[v["run"]]
			if !ok {
				return fmt.Errorf("choose a saved run")
			}
			m.queueWorkflow("inspect-run", id)
			return nil
		})
		m.dialog.form.Choices = map[string][]string{"run": options}
	case "review":
		if w.run == nil {
			w.notice = "Open a run first"
			return nil
		}
		pending := []string{}
		for _, s := range w.run.Steps {
			if s.Status == "waiting_review" {
				pending = append(pending, s.ID)
			}
		}
		if len(pending) == 0 {
			w.notice = "No pending review"
			return nil
		}
		resolved := ""
		for _, step := range w.run.Steps {
			if step.ID == pending[0] {
				resolved = prettyToolArguments(string(step.Args))
				break
			}
		}
		m.openForm("Review operation", "Approval executes this resolved operation. Completed operations are not repeated.", "Continue", []formField{{Key: "step", Label: "Waiting node", Value: pending[0], ReadOnly: true}, {Key: "resolved", Label: "Resolved arguments", Value: resolved, Multiline: true, ReadOnly: true}, {Key: "decision", Label: "Decision", Value: "Reject"}}, func(v map[string]string) error {
			m.queueWorkflow("review-"+strings.ToLower(v["decision"]), v["step"])
			return nil
		})
		m.dialog.form.Choices = map[string][]string{"decision": {"Reject", "Approve"}}
	case "layout":
		if w.positionsLocked {
			w.notice = "Unlock node positions before auto-layout"
			return nil
		}
		w.graph = m.workflowGraph(graph.Model{Selected: w.graph.Selected})
		m.persistWorkflowLayout()
	case "undo":
		w.editor.Undo()
		w.graph = m.workflowGraph(w.graph)
	case "redo":
		w.editor.Redo()
		w.graph = m.workflowGraph(w.graph)
	case "draft":
		w.runID = ""
		w.run = nil
		w.graph = m.workflowGraph(graph.Model{})
		w.offset = 0
	}
	return nil
}

func (m *model) workflowAddOperation(name string) {
	w := m.workflows
	if w.busy || w.run != nil {
		return
	}
	n, err := w.registry.Node(name)
	if err != nil {
		w.notice = err.Error()
		return
	}
	draft := w.editor.Source()
	m.openForm("Add operation", name, "Add", []formField{{Key: "id", Label: "Unique node ID", Value: fmt.Sprintf("step_%d", len(w.editor.Definition.Steps)+1)}}, func(v map[string]string) error {
		if w.busy || w.run != nil || w.editor.Source() != draft {
			return fmt.Errorf("draft changed; add the node again")
		}
		id := strings.TrimSpace(v["id"])
		if id == "" || strings.HasPrefix(id, "@") || strings.Contains(id, "/") {
			return fmt.Errorf("enter a node ID without / or a leading @")
		}
		d := w.editor.Clone()
		for _, s := range d.Steps {
			if s.ID == id {
				return fmt.Errorf("node ID already exists")
			}
		}
		d.Steps = append(d.Steps, automation.Step{ID: id, Tool: n.Name, NodeVersion: n.Version, Args: json.RawMessage(`{}`)})
		w.editor.Replace(d)
		w.graph.Selected = id
		w.graph = m.workflowGraph(w.graph)
		m.queueWorkflow("edit", "")
		return nil
	})
}

func (m *model) workflowEdit() {
	w := m.workflows
	if m.workflowSelectedSchedule() != nil {
		m.workflowSchedule()
		return
	}
	idx := -1
	for i, s := range w.editor.Definition.Steps {
		if s.ID == w.graph.Selected {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	s := w.editor.Definition.Steps[idx]
	n, err := w.registry.Node(s.Tool)
	if err != nil {
		m.openDialog("Node", []string{err.Error()})
		return
	}
	schema := schemaform.Parse(n.InputSchema)
	if w.inspectorOpen && w.inspect && w.inspectorTab == 0 {
		delete(schema.Properties, "code")
	}
	draft := w.editor.Source()
	m.openForm(s.ID+" / "+s.Tool, n.Description+" · * required. Use JSON objects for references.", "Apply", schema.Fields(s.Args), func(v map[string]string) error {
		if w.run != nil || w.busy || w.editor.Source() != draft {
			return fmt.Errorf("draft changed; reopen node arguments")
		}
		args, err := schema.Values(s.Args, v)
		if err != nil {
			return err
		}
		d := w.editor.Clone()
		d.Steps[idx].Args = args
		w.editor.Replace(d)
		w.graph = m.workflowGraph(w.graph)
		return nil
	})
	m.dialog.form.Choices = schema.Choices()
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
			w.notice = "Connection cancelled"
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
			w.notice = "Viewing a frozen run · D returns to the editable draft"
			return m, nil
		}
		return m, m.workflowCommand(action)
	}
	return m, nil
}
func (m *model) workflowCanvasSize() (int, int) {
	area := m.screenPanes(m.workflows.inspectorOpen).Main
	layout := m.panelGeometry(area.Width, 1, false, false)
	return max(layout.Body.Width, 1), max(layout.Body.Height, 1)
}
func (m *model) workflowScreen() string {
	w := m.workflows
	cw, ch := m.workflowCanvasSize()
	m.workflowEnsureSelection()
	if w.run == nil {
		w.graph = m.workflowGraph(w.graph)
	}
	styles := popupSurface()
	normal := styles.Normal.Background(lipgloss.Color(currentPalette().background))
	canvas := w.graph
	if w.overview {
		canvas = w.graph.Overview(cw)
	}
	left := canvas.View(cw, ch, normal, styles.Selected)
	if len(canvas.Nodes) == 0 {
		left = cellFrame("No workflow open.\nCtrl+O opens a recipe · H saved runs\nA adds a node · I requests an AI proposal", cw, ch)
	}
	title := w.editor.Definition.Name
	if w.editor.Dirty() {
		title += " *"
	}
	if w.run != nil {
		title = w.run.Definition.Name + " / " + w.run.Status
	}
	mode := "Workflow"
	if w.positionsLocked {
		mode += " [positions locked]"
	}
	regions := m.screenPanes(w.inspectorOpen)
	split := w.inspectorOpen && !regions.Covered
	leftWidth := regions.Main.Width
	rows := m.inspectionRows(mode+" / "+cleanText(title), m.styles.dim.Render(cleanText(w.notice)), left, leftWidth, !w.inspect, false)
	if w.inspectorOpen {
		width := regions.Side.Width
		content := m.workflowInspectorContent()
		tabs := m.workflowInspectorTabs()
		capacity := max(1, ch-len(tabs.Rows)-1)
		w.offset = min(w.offset, max(0, len(content)-capacity))
		visible := content[w.offset:min(len(content), w.offset+capacity)]
		inner := m.panelGeometry(width, 1, true, false).Body.Width
		body := strings.Join(tabs.Rows, "\n") + "\n\n" + strings.Join(popup.ScrollRows(visible, inner, w.offset, len(content), m.styles.dim), "\n")
		heading := "Inspect / " + cleanText(w.graph.Selected)
		if m.workflowSelectedSchedule() != nil && w.run == nil {
			heading = "Inspect / Schedule"
		}
		if w.rawDetails {
			heading += " / Raw"
		}
		if w.inspect {
			heading += " [focused]"
		}
		shortcuts := []popup.Shortcut{{Key: "Tab", Action: "focus inspector"}, {Key: "1–3", Action: "tabs"}}
		if w.inspect {
			shortcuts = []popup.Shortcut{{Key: "Esc", Action: "close"}, {Key: "Tab", Action: "parent"}, {Key: "1–3", Action: "tabs"}}
		}
		if w.inspect && w.run == nil && !w.busy && w.inspectorTab != 1 {
			shortcuts = []popup.Shortcut{{Key: "Esc", Action: "close"}, {Key: "Tab", Action: "parent"}, {Key: "E", Action: "edit"}, {Key: "1–3", Action: "tabs"}}
		}
		hint := m.inspectionHints(width, shortcuts...)
		right := m.inspectionRows(heading, hint, body, width, w.inspect, true)
		if split {
			for i := range rows {
				rows[i] += right[i]
			}
		} else {
			rows = right
		}
	}
	content := cellLine(m.menuLine(), m.termW) + "\n" + strings.Join(rows, "\n") + "\n" + m.actionBar()
	content = cellFrame(content, m.termW, m.termH)
	if m.dialog != nil {
		content = m.workflowDialog(content)
	}
	if m.header.menu != "" {
		content = m.overlayHeader(content)
	}
	return content
}

// Shared dialogs overlay pane-local content; their hit tests add settingsArea's
// screen origin. Keep that same origin when composing the workflow's full screen.
func (m *model) workflowDialog(content string) string {
	left, top, width, height := m.settingsArea()
	base := strings.Split(content, "\n")
	pane := make([]string, height)
	for i := range pane {
		if top+i < len(base) {
			pane[i] = cellLine(ansi.Cut(base[top+i], left, left+width), width)
		}
	}
	rows := strings.Split(m.overlayDialog(strings.Join(pane, "\n")), "\n")
	for i, row := range rows {
		y := top + i
		if y >= len(base) || i >= height {
			break
		}
		base[y] = cellLine(ansi.Cut(base[y], 0, left), left) + cellLine(row, width) + ansi.Cut(base[y], left+width, m.termW)
	}
	return cellFrame(strings.Join(base, "\n"), m.termW, m.termH)
}
func (m *model) workflowMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	w := m.workflows
	mouse := msg.Mouse()
	if mouse.Y == m.termH-1 && mouse.Button == tea.MouseLeft && isMouseClick(msg) {
		for _, cell := range m.footerCells() {
			if mouse.X >= cell.start && mouse.X < cell.end && cell.action.enabled {
				return m.handleKey(tea.KeyPressMsg{Code: cell.action.key, Mod: cell.action.mod})
			}
		}
		return m, nil
	}
	cw, ch := m.workflowCanvasSize()
	regions := m.screenPanes(w.inspectorOpen)
	canvasLayout := m.panelGeometry(regions.Main.Width, 1, false, false)
	sideLayout := m.panelGeometry(regions.Side.Width, 1, true, false)
	x, y := mouse.X-canvasLayout.Body.X, mouse.Y-regions.Main.Y-canvasLayout.Body.Y
	inspectorHeader := len(m.workflowInspectorTabs().Rows) + 1
	pane := w.inspectorOpen && regions.Side.Contains(mouse.X, mouse.Y)
	if pane && sideLayout.CloseHit.Contains(mouse.X-regions.Side.X, mouse.Y-regions.Side.Y) && mouse.Button == tea.MouseLeft && isMouseClick(msg) {
		w.inspectorOpen, w.inspect = false, false
		w.thumb = scrollDrag{}
		return m, nil
	}
	if w.thumb.active {
		w.offset = popup.ScrollOffset(y-inspectorHeader, max(1, ch-inspectorHeader), len(m.workflowInspectorContent()), w.thumb.grab)
		if _, ok := msg.(tea.MouseReleaseMsg); ok {
			w.thumb = scrollDrag{}
		}
		return m, nil
	}
	if pane && mouse.Button == tea.MouseLeft && isMouseClick(msg) {
		tabs := m.workflowInspectorTabs()
		if tab := tabs.Hit(mouse.X-regions.Side.X-sideLayout.Body.X, y); tab >= 0 {
			m.workflowSetInspectorTab(tab)
			w.inspect = true
			return m, nil
		}
	}

	if _, ok := msg.(tea.MouseReleaseMsg); ok {
		dragged := w.dragging
		w.dragging = false
		w.panning = false
		if dragged {
			m.persistWorkflowLayout()
		}
		return m, nil
	}
	if w.panning {
		w.graph.Pan.X -= x - w.last.X
		w.graph.Pan.Y -= y - w.last.Y
		w.last = graph.Point{X: x, Y: y}
		return m, nil
	}
	if w.dragging {
		w.graph.Move(x-w.last.X, y-w.last.Y)
		w.last = graph.Point{X: x, Y: y}
		return m, nil
	}
	if y < 0 || y >= ch {
		return m, nil
	}
	wheel := mouse.Button == tea.MouseWheelDown || mouse.Button == tea.MouseWheelUp || mouse.Button == tea.MouseWheelLeft || mouse.Button == tea.MouseWheelRight
	if wheel {
		delta := 3
		if mouse.Button == tea.MouseWheelUp || mouse.Button == tea.MouseWheelLeft {
			delta = -3
		}
		if pane {
			if y >= inspectorHeader {
				w.offset = max(0, w.offset+delta)
			}
		} else if mouse.Button == tea.MouseWheelLeft || mouse.Button == tea.MouseWheelRight || mouse.Mod&tea.ModShift != 0 {
			w.graph.Pan.X += delta
		} else {
			w.graph.Pan.Y += delta
		}
		return m, nil
	}
	if pane {
		if mouse.Button == tea.MouseLeft && isMouseClick(msg) {
			w.inspect = true
			if y >= inspectorHeader && y < ch && mouse.X == regions.Side.X+sideLayout.Body.X+sideLayout.Body.Width-1 {
				total := len(m.workflowInspectorContent())
				top, size := popup.ScrollThumb(max(1, ch-inspectorHeader), total, w.offset)
				if y-inspectorHeader >= top && y-inspectorHeader < top+size {
					w.thumb = scrollDrag{active: true, grab: y - inspectorHeader - top}
				} else {
					w.offset = popup.ScrollOffset(y-inspectorHeader, max(1, ch-inspectorHeader), total, 0)
				}
			}
		}
		return m, nil
	}
	if x < 0 || x >= cw {
		return m, nil
	}
	if mouse.Button == tea.MouseMiddle && isMouseClick(msg) {
		w.panning = true
		w.last = graph.Point{X: x, Y: y}
		return m, nil
	}
	if mouse.Button == tea.MouseLeft && isMouseClick(msg) {
		canvas := w.graph
		if w.overview {
			canvas = w.graph.Overview(cw)
		}
		if !w.overview {
			if id, port, output := canvas.PortHit(x, y); id != "" {
				w.graph.Selected = id
				if strings.HasPrefix(id, workflowSchedulePrefix) || strings.HasPrefix(w.connectFrom, workflowSchedulePrefix) {
					w.connectFrom = ""
					w.inspectorOpen = true
					w.notice = "Schedule starts the whole recipe; its connections are automatic"
					return m, nil
				}
				m.workflowFocusPort(port, output)
				w.inspect = false
				if w.run != nil || w.busy {
					w.inspectorOpen = true
					w.inspect = true
					return m, nil
				}
				if output {
					w.connectFrom = id
					w.connectPort = port
					w.notice = "Choose an input port · Esc cancels"
				} else if w.connectFrom != "" {
					if (port == "@order") != (w.connectPort == "@order") {
						w.notice = "Connect data ports together, or then → after for execution order"
						return m, nil
					}
					edit := workflowConnectionEdit{From: w.connectFrom, To: id, Pointer: w.connectPort, FixedSource: true, Draft: w.editor.Source()}
					switch {
					case port == "@order":
						edit.Field = "depends_on"
						edit.Pointer = ""
					case strings.HasPrefix(port, "args/"):
						edit.Field = "args"
						edit.Path = strings.TrimPrefix(port, "args")
					case port == "for_each" || strings.HasPrefix(port, "for_each/"):
						edit.Field = "for_each"
						edit.Path = strings.TrimPrefix(port, "for_each")
					case port == "when" || strings.HasPrefix(port, "when/"):
						edit.Field = "when"
						edit.Path = strings.TrimPrefix(port, "when")
					default:
						w.notice = "Choose a named destination port"
						return m, nil
					}
					w.connectFrom = ""
					m.workflowConnectionEdit(edit)
				} else {
					w.notice = "Choose an output port first · C connects by keyboard"
				}
				return m, nil
			}
		}
		id := canvas.Hit(x, y)
		w.inspect = false
		if id != "" {
			w.graph.Selected = id
			w.portSelected = false
			w.offset = 0
			w.dragging = !w.overview && !w.positionsLocked
		} else {
			w.panning = !w.overview
		}
		w.last = graph.Point{X: x, Y: y}
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

// workflowLinkTarget resolves the destination schema after both nodes are chosen.
func (m *model) workflowLinkTarget(value string) {
	var pair []string
	if json.Unmarshal([]byte(value), &pair) != nil || len(pair) != 2 {
		return
	}
	m.workflowConnectionTarget(pair[0], pair[1], nil)
}
