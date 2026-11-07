package tui

import (
	tea "charm.land/bubbletea/v2"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/schemaform"
	"github.com/keshon/gektor/internal/tui/settings"
	"strings"
)

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
		w.setNotice(pane.NoticeInfo, "Operation in progress · Esc stops it")
		return nil
	}
	if w.run != nil && workflowRequiresDraft(action) {
		w.setNotice(pane.NoticeInfo, "Frozen run · D returns to the editable draft")
		return nil
	}
	if action == "chat" {
		w.active = false
		return nil
	}
	if m.workflowSelectedSchedule() != nil && (action == "behavior" || action == "connections" || action == "link") {
		w.setNotice(pane.NoticeInfo, "Schedule starts the whole recipe. Enter edits the trigger; connections are automatic.")
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
		return m.workflowSetupForm()

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
		w.setNotice(pane.NoticeInfo, "Stopping…")
	case "new", "open":
		m.workflowRecipePath(action)
	case "save":
		m.workflowSaveForm()
	case "add":
		return m.workflowAddNodeForm()

	case "edit":
		m.workflowEdit()
	case "behavior":
		return m.workflowBehaviorForm()

	case "ai":
		return m.workflowProposalForm()

	case "recipe":
		return m.workflowSourceForm()

	case "link":
		return m.workflowConnectForm()

	case "connections":
		m.workflowConnections()

	case "delete":
		return m.workflowDeleteNodeForm()

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
		m.openWorkflowRuns(runs)
	case "review":
		return m.workflowReviewForm()

	case "layout":
		if w.positionsLocked {
			w.setNotice(pane.NoticeWarning, "Unlock node positions before auto-layout")
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

// workflowRequiresDraft identifies actions unavailable while inspecting a frozen run.
func workflowRequiresDraft(action string) bool {
	switch action {
	case "edit", "add", "link", "connections", "delete", "recipe", "name", "inputs", "outputs", "ai", "behavior", "undo", "redo", "save", "run", "validate":
		return true
	default:
		return false
	}
}

func (m *model) workflowRecipePath(action string) {
	w := m.workflows
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
}

func (m *model) workflowSaveForm() {
	w := m.workflows
	m.openForm("Save workflow", "Writes the recipe; canvas positions stay in your user settings.", "Save", []formField{{Key: "path", Label: "Recipe path", Value: w.editor.Path}}, func(v map[string]string) error {
		if strings.TrimSpace(v["path"]) == "" {
			return fmt.Errorf("enter a path")
		}
		m.queueWorkflow("save-path", v["path"])
		return nil
	})
}

func (m *model) workflowAddOperation(name string) {
	w := m.workflows
	if w.busy || w.run != nil {
		return
	}
	n, err := w.registry.Node(name)
	if err != nil {
		w.setNotice(pane.NoticeError, err.Error())
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

// workflowLinkTarget resolves the destination schema after both nodes are chosen.
func (m *model) workflowLinkTarget(value string) {
	var pair []string
	if json.Unmarshal([]byte(value), &pair) != nil || len(pair) != 2 {
		return
	}
	m.workflowConnectionTarget(pair[0], pair[1], nil)
}
