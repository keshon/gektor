package workflow

import (
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/tui/screen"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/schemaform"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
)

func (m *Model) command(action string) tea.Cmd {
	if !m.active {
		return nil
	}
	w := m
	if action == "inspector" {
		w.inspectorOpen = !w.inspectorOpen
		w.inspect = w.inspectorOpen
		w.thumb = scrollDrag{}
		return nil
	}
	if action == "lock-positions" {
		w.positionsLocked = !w.positionsLocked
		w.dragging = false
		m.persistLayout()
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
	if m.selectedSchedule() != nil && (action == "behavior" || action == "connections" || action == "link") {
		w.setNotice(pane.NoticeInfo, "Schedule starts the whole recipe. Enter edits the trigger; connections are automatic.")
		return nil
	}
	fail := func(err error) {
		m.dialogs.OpenDialog(screen.Dialog{Title: "Workflow", Lines: []string{err.Error()}})
	}
	switch action {
	case "schedule":
		m.schedule()
	case "schedule-add":
		m.scheduleStart(true)
	case "schedule-refresh":
		return m.scheduleRefresh()
	case "help":
		_, cmd := m.key(tea.KeyPressMsg{Code: tea.KeyF1})
		return cmd
	case "setup":
		return m.setupForm()

	case "name":
		if w.run == nil {
			m.recipeName()
		}
	case "inputs", "outputs":
		if w.run == nil {
			m.parameters(action)
		}
	case "stop":
		if w.cancel != nil {
			w.cancel()
		}
		w.setNotice(pane.NoticeInfo, "Stopping…")
	case "new", "open":
		m.recipePath(action)
	case "save":
		m.saveForm()
	case "add":
		return m.addNodeForm()

	case "edit":
		m.edit()
	case "behavior":
		return m.behaviorForm()

	case "ai":
		return m.proposalForm()

	case "recipe":
		return m.sourceForm()

	case "link":
		return m.connectForm()

	case "connections":
		m.connections()

	case "delete":
		return m.deleteNodeForm()

	case "run-json", "validate-json":
		base := strings.TrimSuffix(action, "-json")
		m.inputForm(workflowInvocation{Action: base, Draft: w.editor.Source(), Mode: "JSON"})
	case "validate", "run":
		m.runInputs(action)
	case "runs":
		s, err := store.OpenStore(userdirs.Sub("workflows"), m.config.WS.Root())
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
		m.openRuns(runs)
	case "review":
		return m.reviewForm()

	case "layout":
		if w.positionsLocked {
			w.setNotice(pane.NoticeWarning, "Unlock node positions before auto-layout")
			return nil
		}
		w.graph = m.buildGraph(graph.Model{Selected: w.graph.Selected})
		m.persistLayout()
	case "undo":
		w.editor.Undo()
		w.graph = m.buildGraph(w.graph)
	case "redo":
		w.editor.Redo()
		w.graph = m.buildGraph(w.graph)
	case "draft":
		w.runID = ""
		w.run = nil
		w.graph = m.buildGraph(graph.Model{})
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

func (m *Model) recipePath(action string) *settings.Model {
	w := m
	fields := []formField{{Key: "path", Label: "Recipe path (relative to workspace)", Value: w.editor.Path}}
	var fileFields map[string]settings.FilePicker
	if action == "open" {
		fileFields = map[string]settings.FilePicker{"path": {Root: m.config.WS.Root(), Extensions: []string{".json"}}}
	}
	if action == "new" {
		fields[0].Value = "recipe.json"
	}
	if w.editor.Dirty() {
		fields = append(fields, formField{Key: "discard", Label: "Unsaved changes: type discard to replace"})
	}
	return m.dialogs.OpenForm(screen.Form{
		Title:      "Workflow / " + action,
		Hint:       "Workspace: " + m.config.WS.Root(),
		ApplyLabel: "Continue",
		Fields:     fields,
		FileFields: fileFields,
		Apply: func(v map[string]string) error {
			if w.editor.Dirty() && v["discard"] != "discard" {
				return fmt.Errorf("save first, or type discard")
			}
			if strings.TrimSpace(v["path"]) == "" {
				return fmt.Errorf("enter a recipe path")
			}
			if _, err := m.config.WS.Relative(v["path"]); err != nil {
				return err
			}
			m.queueAction(action+"-path", v["path"])
			return nil
		},
	})
}

func (m *Model) saveForm() {
	w := m
	m.dialogs.OpenForm(screen.Form{
		Title:      "Save workflow",
		Hint:       "Writes the recipe; canvas positions stay in your user settings.",
		ApplyLabel: "Save",
		Fields:     []formField{{Key: "path", Label: "Recipe path", Value: w.editor.Path}},
		Apply: func(v map[string]string) error {
			if strings.TrimSpace(v["path"]) == "" {
				return fmt.Errorf("enter a path")
			}
			m.queueAction("save-path", v["path"])
			return nil
		},
	})
}

func (m *Model) addOperation(name string) {
	w := m
	if w.busy || w.run != nil {
		return
	}
	n, err := w.registry.Node(name)
	if err != nil {
		w.setNotice(pane.NoticeError, err.Error())
		return
	}
	draft := w.editor.Source()
	m.dialogs.OpenForm(screen.Form{
		Title:      "Add operation",
		Hint:       name,
		ApplyLabel: "Add",
		Fields:     []formField{{Key: "id", Label: "Unique node ID", Value: fmt.Sprintf("step_%d", len(w.editor.Definition.Steps)+1)}},
		Apply: func(v map[string]string) error {
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
			d.Steps = append(d.Steps, workflow.Step{ID: id, Tool: n.Name, NodeVersion: n.Version, Args: json.RawMessage(`{}`)})
			w.editor.Replace(d)
			w.graph.Selected = id
			w.graph = m.buildGraph(w.graph)
			m.queueAction("edit", "")
			return nil
		},
	})
}

func (m *Model) edit() {
	w := m
	if m.selectedSchedule() != nil {
		m.schedule()
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
		m.dialogs.OpenDialog(screen.Dialog{Title: "Node", Lines: []string{err.Error()}})
		return
	}
	schema := schemaform.Parse(n.InputSchema)
	if w.inspectorOpen && w.inspect && w.inspectorTab == 0 {
		delete(schema.Properties, "code")
	}
	draft := w.editor.Source()
	m.dialogs.OpenForm(screen.Form{
		Title:      s.ID + " / " + s.Tool,
		Hint:       n.Description + " · * required. Use JSON objects for references.",
		ApplyLabel: "Apply",
		Fields:     schema.Fields(s.Args),
		Apply: func(v map[string]string) error {
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
			w.graph = m.buildGraph(w.graph)
			return nil
		},
		Choices: schema.Choices(),
	})

}

// workflowLinkTarget resolves the destination schema after both nodes are chosen.
func (m *Model) linkTarget(value string) {
	var pair []string
	if json.Unmarshal([]byte(value), &pair) != nil || len(pair) != 2 {
		return
	}
	m.connectionTarget(pair[0], pair[1], nil)
}
