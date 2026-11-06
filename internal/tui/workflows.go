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
	thumb                  scrollDrag
	active, busy, inspect  bool
	overview               bool
	inspectorOpen, panning bool
	connectFrom            string
	editor                 *editor.Editor
	graph                  graph.Model
	registry               *automation.Registry
	notice                 string
	offset                 int
	dragging               bool
	last                   graph.Point
	cancel                 context.CancelFunc
	events                 chan tea.Msg
	run                    *automation.Run
	runID                  string
}
type workflowAction struct{ action, value string }
type workflowResult struct {
	text         string
	err          error
	source, path string
	action       string
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
	fail := func(err error) { m.openDialog("Workflow", []string{err.Error()}) }
	switch action {
	case "help":
		_, cmd := m.workflowKey(tea.KeyPressMsg{Code: tea.KeyF1})
		return cmd
	case "setup":
		if w.run != nil {
			return nil
		}
		m.openForm("Recipe settings", "Choose a part of the recipe to edit.", "Open", []formField{{Key: "section", Label: "Section", Value: "Name"}}, func(v map[string]string) error {
			actions := map[string]string{"Name": "name", "Inputs": "inputs", "Outputs": "outputs", "Complete JSON": "recipe"}
			action, ok := actions[v["section"]]
			if !ok {
				return fmt.Errorf("choose a section")
			}
			m.queueWorkflow(action, "")
			return nil
		})
		m.dialog.form.Choices = map[string][]string{"section": {"Name", "Inputs", "Outputs", "Complete JSON"}}
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
		names := w.registry.Names()
		if len(names) == 0 {
			return nil
		}
		m.openForm("Add node", "Operations come from the workflow registry.", "Add", []formField{{Key: "type", Label: "Operation", Value: names[0]}, {Key: "id", Label: "Unique node ID", Value: fmt.Sprintf("step_%d", len(w.editor.Definition.Steps)+1)}}, func(v map[string]string) error {
			id := strings.TrimSpace(v["id"])
			if id == "" {
				return fmt.Errorf("node ID is required")
			}
			d := w.editor.Clone()
			for _, s := range d.Steps {
				if s.ID == id {
					return fmt.Errorf("node ID already exists")
				}
			}
			n, err := w.registry.Node(v["type"])
			if err != nil {
				return err
			}
			d.Steps = append(d.Steps, automation.Step{ID: id, Tool: n.Name, NodeVersion: n.Version, Args: json.RawMessage(`{}`)})
			w.editor.Replace(d)
			w.graph.Selected = id
			w.graph = w.editor.Graph(w.graph)
			m.queueWorkflow("edit", "")
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
				w.graph = w.editor.Graph(w.graph)
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
			w.graph = w.editor.Graph(w.graph)
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
			w.graph = w.editor.Graph(w.graph)
			return nil
		})
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
		for _, r := range runs {
			options = append(options, r.ID+" · "+r.Name+" · "+r.Status)
		}
		if len(options) == 0 {
			w.notice = "No runs in this workspace"
			return nil
		}
		m.openForm("Workflow runs", "Open an immutable execution record.", "Open", []formField{{Key: "run", Label: "Run", Value: options[len(options)-1]}}, func(v map[string]string) error {
			id, _, _ := strings.Cut(v["run"], " · ")
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
		w.graph = w.editor.Graph(graph.Model{Selected: w.graph.Selected})
		m.persistWorkflowLayout()
	case "undo":
		w.editor.Undo()
		w.graph = w.editor.Graph(w.graph)
	case "redo":
		w.editor.Redo()
		w.graph = w.editor.Graph(w.graph)
	case "draft":
		w.runID = ""
		w.run = nil
		w.graph = w.editor.Graph(graph.Model{})
		w.offset = 0
	}
	return nil
}

func (m *model) workflowEdit() {
	w := m.workflows
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
	m.openForm(s.ID+" / "+s.Tool, n.Description+" · * required. Use JSON objects for references.", "Apply", schema.Fields(s.Args), func(v map[string]string) error {
		args, err := schema.Values(s.Args, v)
		if err != nil {
			return err
		}
		d := w.editor.Clone()
		d.Steps[idx].Args = args
		w.editor.Replace(d)
		w.graph = w.editor.Graph(w.graph)
		return nil
	})
	m.dialog.form.Choices = schema.Choices()
}

func (m *model) workflowKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	w := m.workflows
	key := msg.String()
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
			return m, nil
		}
		action = "edit"
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
			w.graph.SelectNext(delta)
			cw, ch := m.workflowCanvasSize()
			w.graph.Reveal(cw, ch)
		}
	case "shift+left":
		w.graph.Move(-2, 0)
	case "shift+right":
		w.graph.Move(2, 0)
	case "shift+up":
		w.graph.Move(0, -1)
	case "shift+down":
		w.graph.Move(0, 1)
	case "alt+left":
		w.graph.Pan.X -= 8
	case "alt+right":
		w.graph.Pan.X += 8
	case "alt+up":
		w.graph.Pan.Y -= 4
	case "alt+down":
		w.graph.Pan.Y += 4
	case "f1":
		m.openDialog("Workflow keys", strings.Split("Ctrl+O open · Ctrl+N new · Ctrl+S save\nA add · Enter edit/inspect · Delete delete\nC connect · B connections · E node behavior · J complete recipe JSON\nM recipe name · U inputs · O outputs\nArrows select · Shift+arrows move · Alt+arrows pan\nDrag background to pan · Shift+wheel horizontal\nClick output then input to connect · Ctrl+T inspector\nTab pane focus · Esc closes focused inspector\nF compact overview · L auto-layout · Ctrl+Z undo · Ctrl+Y redo\nV validate · R run · H saved runs · P review\nD editable draft · I AI proposal\nEsc chat (draft retained) / stop active operation\nF9 main menu · Ctrl+Q quit", "\n"))
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
	rows := 7
	if !m.minimal {
		rows++
	}
	width := max(1, m.termW-2)
	if m.workflows.inspectorOpen && m.termW >= inspectorSplitWidth {
		width = max(1, (m.termW-3)*2/3)
	}
	return width, max(1, m.termH-rows)
}
func (m *model) workflowScreen() string {
	w := m.workflows
	cw, ch := m.workflowCanvasSize()
	rw := max(1, m.termW-cw-3)
	if w.run == nil {
		w.graph = w.editor.Graph(w.graph)
	}
	styles := popupSurface()
	normal := styles.Normal.Background(lipgloss.Color(currentPalette().background))
	canvas := w.graph
	if w.overview {
		canvas = w.graph.Overview(cw)
	}
	left := canvas.View(cw, ch, normal, styles.Selected)
	if len(w.graph.Nodes) == 0 {
		left = cellFrame("  No nodes yet.\n\n  A add a node   Ctrl+O open a recipe", cw, ch)
	}
	split := w.inspectorOpen && m.termW >= inspectorSplitWidth
	body := left
	if w.inspectorOpen {
		paneWidth := rw
		if !split {
			paneWidth = cw
		}
		detail := m.workflowDetailRows()
		detail[0] = cellLine(detail[0], max(0, paneWidth-4)) + " [x]"
		content := m.workflowInspectorContent()
		capacity := max(1, ch-1)
		w.offset = min(w.offset, max(0, len(content)-capacity))
		visible := content[w.offset:min(len(content), w.offset+capacity)]
		paneStyle := styles.Normal
		right := append([]string{paneStyle.Bold(true).Render(detail[0])}, popup.ScrollRows(visible, paneWidth, w.offset, len(content), paneStyle)...)
		right = strings.Split(cellFrame(strings.Join(right, "\n"), paneWidth, ch), "\n")
		if split {
			lr := strings.Split(left, "\n")
			rows := make([]string, ch)
			for i := range rows {
				separator := styles.Border.Render("│")
				rows[i] = lr[i] + separator + right[i]
			}
			body = strings.Join(rows, "\n")
		} else {
			body = strings.Join(right, "\n")
		}
	}
	if m.dialog != nil {
		body = m.overlayDialog(body)
	}
	bodyRows := strings.Split(body, "\n")
	for i := range bodyRows {
		edge := " "
		if !m.minimal {
			edge = "│"
		}
		bodyRows[i] = m.styles.dim.Render(edge) + cellLine(bodyRows[i], max(1, m.termW-2)) + m.styles.dim.Render(edge)
	}
	body = strings.Join(bodyRows, "\n")
	title := w.editor.Definition.Name
	if w.editor.Dirty() {
		title += " *"
	}
	if w.run != nil {
		title = w.run.Definition.Name + " · frozen run " + w.run.RunID
	}
	if w.busy {
		title += " · running"
	}
	rule := m.styles.dim.Render(strings.Repeat("─", max(1, m.termW)))
	header := cellLine(" Workflows · sequential · "+cleanText(title), m.termW)
	notice := cellLine(" "+cleanText(w.notice), m.termW)
	top, divider, bottom := rule, rule, rule
	bodyBottom := ""
	if !m.minimal && m.termW >= 4 {
		frameRow := func(text string) string {
			return m.styles.dim.Render("│") + cellLine(text, m.termW-2) + m.styles.dim.Render("│")
		}
		splitRule := func(left, joint, right string) string {
			return m.styles.dim.Render(left + strings.Repeat("─", cw) + joint + strings.Repeat("─", rw) + right)
		}
		top = m.styles.dim.Render("┌" + strings.Repeat("─", m.termW-2) + "┐")
		if split {
			divider = splitRule("├", "┬", "┤")
			bodyBottom = "\n" + splitRule("├", "┴", "┤")
		} else {
			divider = m.styles.dim.Render("├" + strings.Repeat("─", m.termW-2) + "┤")
			bodyBottom = "\n" + divider
		}
		bottom = m.styles.dim.Render("└" + strings.Repeat("─", m.termW-2) + "┘")
		header = frameRow(" Workflows · sequential · " + cleanText(title))
		notice = frameRow(" " + cleanText(w.notice))
	}
	content := cellLine(m.menuLine(), m.termW) + "\n" + top + "\n" + header + "\n" + divider + "\n" + body + bodyBottom + "\n" + notice + "\n" + bottom + "\n" + m.actionBar()
	content = cellFrame(content, m.termW, m.termH)
	if m.header.menu != "" {
		content = m.overlayHeader(content)
	}
	return content
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
	x, y := mouse.X-1, mouse.Y-4
	pane := w.inspectorOpen && (m.termW < inspectorSplitWidth || x > cw)
	if w.thumb.active {
		w.offset = popup.ScrollOffset(y-1, max(1, ch-1), len(m.workflowInspectorContent()), w.thumb.grab)
		if _, ok := msg.(tea.MouseReleaseMsg); ok {
			w.thumb = scrollDrag{}
		}
		return m, nil
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
			w.offset = max(0, w.offset+delta)
		} else if mouse.Button == tea.MouseWheelLeft || mouse.Button == tea.MouseWheelRight || mouse.Mod&tea.ModShift != 0 {
			w.graph.Pan.X += delta
		} else {
			w.graph.Pan.Y += delta
		}
		return m, nil
	}
	if pane {
		if mouse.Button == tea.MouseLeft && isMouseClick(msg) {
			if y == 0 && mouse.X >= m.termW-5 {
				w.inspectorOpen = false
				w.inspect = false
				w.thumb = scrollDrag{}
				return m, nil
			}
			w.inspect = true
			if y > 0 && x >= m.termW-4 {
				total := len(m.workflowInspectorContent())
				top, size := popup.ScrollThumb(max(1, ch-1), total, w.offset)
				if y-1 >= top && y-1 < top+size {
					w.thumb = scrollDrag{active: true, grab: y - 1 - top}
				} else {
					w.offset = popup.ScrollOffset(y-1, max(1, ch-1), total, 0)
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
			if id, output := canvas.PortAt(x, y); id != "" {
				w.graph.Selected = id
				w.offset = 0
				w.inspect = false
				if w.run != nil || w.busy {
					w.inspectorOpen = true
					w.inspect = true
					return m, nil
				}
				if output {
					w.connectFrom = id
					w.notice = "Choose an input port · Esc cancels"
				} else if w.connectFrom != "" {
					pair, _ := json.Marshal([]string{w.connectFrom, id})
					w.connectFrom = ""
					m.workflowLinkTarget(string(pair))
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
			w.offset = 0
			w.dragging = !w.overview
		} else {
			w.panning = !w.overview
		}
		w.last = graph.Point{X: x, Y: y}
	}
	return m, nil
}
func (m *model) workflowInspectorContent() []string {
	cw, _ := m.workflowCanvasSize()
	width := cw
	if m.termW >= inspectorSplitWidth {
		width = m.termW - cw - 3
	}
	var out []string
	for _, line := range m.workflowDetailRows()[1:] {
		out = append(out, strings.Split(ansi.Hardwrap(line, max(1, width-1), true), "\n")...)
	}
	return out
}

func (m *model) workflowDetailRows() []string {
	w := m.workflows
	label := "Inspector · Tab focus"
	if w.inspect {
		label = "Inspector [focused]"
	}
	detail := []string{label, "", w.graph.Selected}
	if w.run != nil {
		detail = append(detail, "Run: "+w.run.Status)
		for _, s := range w.run.Steps {
			if s.ID != w.graph.Selected {
				continue
			}
			detail = append(detail, "Node: "+s.Status, "", "Resolved arguments")
			detail = append(detail, strings.Split(prettyToolArguments(string(s.Args)), "\n")...)
			detail = append(detail, "", "Output")
			detail = append(detail, strings.Split(cleanText(prettyToolArguments(s.Output)), "\n")...)
			if s.Error != "" {
				detail = append(detail, "", "Error: "+cleanText(s.Error))
			}
			for _, a := range s.Attempts {
				line := fmt.Sprintf("Attempt %d · %s", a.Number, a.Started.Format("15:04:05"))
				if a.Finished != nil {
					line += " · " + a.Finished.Sub(a.Started).String()
				}
				detail = append(detail, line)
			}
			if len(s.Iterations) > 0 {
				b, _ := json.MarshalIndent(s.Iterations, "", "  ")
				detail = append(detail, "", "Iterations")
				detail = append(detail, strings.Split(string(b), "\n")...)
			}
		}

	} else {
		detail = append(detail, "", "Connections · B edits")
		for _, c := range w.editor.Connections(w.graph.Selected) {
			detail = append(detail, c.Label())
		}
		detail = append(detail, "")
		for _, s := range w.editor.Definition.Steps {
			if s.ID == w.graph.Selected {
				b, _ := json.MarshalIndent(s, "", "  ")
				detail = append(detail, strings.Split(string(b), "\n")...)
			}
		}
	}
	return detail
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
