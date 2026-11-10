package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/schemaform"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
)

func (m *Model) command(action string) tea.Cmd {
	if !m.active {
		return nil
	}
	if action == "inspector" {
		m.inspectorOpen = !m.inspectorOpen
		m.inspect = m.inspectorOpen
		m.thumb = scrollDrag{}
		return nil
	}
	if action == "lock-positions" {
		m.positionsLocked = !m.positionsLocked
		m.dragging = false
		m.persistLayout()
		return nil
	}
	if action == "raw-details" {
		m.rawDetails = !m.rawDetails
		m.offset = 0
		return nil
	}
	if action == "overview" {
		m.overview = !m.overview
		return nil
	}
	if action == "" {
		return nil
	}

	if m.busy && action != "stop" {
		m.setNotice(pane.NoticeInfo, "Operation in progress · Esc stops it")
		return nil
	}
	if m.run != nil && workflowRequiresDraft(action) {
		m.setNotice(pane.NoticeInfo, "Frozen run · D returns to the editable draft")
		return nil
	}
	if action == "chat" {
		m.active = false
		return nil
	}
	if m.selectedSchedule() != nil && (action == "behavior" || action == "connections" || action == "link") {
		m.setNotice(pane.NoticeInfo, "Schedule starts the whole recipe. Enter edits the trigger; connections are automatic.")
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
		if m.run == nil {
			m.recipeName()
		}
	case "inputs", "outputs":
		if m.run == nil {
			m.parameters(action)
		}
	case "stop":
		if m.cancel != nil {
			m.cancel()
		}
		m.setNotice(pane.NoticeInfo, "Stopping…")
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
		m.inputForm(workflowInvocation{Action: base, Draft: m.editor.Source(), Mode: "JSON"})
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
		if m.positionsLocked {
			m.setNotice(pane.NoticeWarning, "Unlock node positions before auto-layout")
			return nil
		}
		m.graph = m.buildGraph(graph.Model{Selected: m.graph.Selected})
		m.persistLayout()
	case "undo":
		m.editor.Undo()
		m.graph = m.buildGraph(m.graph)
	case "redo":
		m.editor.Redo()
		m.graph = m.buildGraph(m.graph)
	case "draft":
		m.runID = ""
		m.run = nil
		m.graph = m.buildGraph(graph.Model{})
		m.offset = 0
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
	fields := []formField{{Key: "path", Label: "Recipe path (relative to workspace)", Value: m.editor.Path}}
	var fileFields map[string]settings.FilePicker
	if action == "open" {
		fileFields = map[string]settings.FilePicker{"path": {Root: m.config.WS.Root(), Extensions: []string{".json"}}}
	}
	if action == "new" {
		fields[0].Value = "recipe.json"
	}
	if m.editor.Dirty() {
		fields = append(fields, formField{Key: "discard", Label: "Unsaved changes: type discard to replace"})
	}
	return m.dialogs.OpenForm(screen.Form{
		Title:      "Workflow / " + action,
		Hint:       "Workspace: " + m.config.WS.Root(),
		ApplyLabel: "Continue",
		Fields:     fields,
		FileFields: fileFields,
		Apply: func(v map[string]string) error {
			if m.editor.Dirty() && v["discard"] != "discard" {
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
	m.dialogs.OpenForm(screen.Form{
		Title:      "Save workflow",
		Hint:       "Writes the recipe; canvas positions stay in your user settings.",
		ApplyLabel: "Save",
		Fields:     []formField{{Key: "path", Label: "Recipe path", Value: m.editor.Path}},
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
	if m.busy || m.run != nil {
		return
	}
	n, err := m.registry.Node(name)
	if err != nil {
		m.setNotice(pane.NoticeError, err.Error())
		return
	}
	draft := m.editor.Source()
	m.dialogs.OpenForm(screen.Form{
		Title:      "Add operation",
		Hint:       name,
		ApplyLabel: "Add",
		Fields:     []formField{{Key: "id", Label: "Unique node ID", Value: fmt.Sprintf("step_%d", len(m.editor.Definition.Steps)+1)}},
		Apply: func(v map[string]string) error {
			if m.busy || m.run != nil || m.editor.Source() != draft {
				return fmt.Errorf("draft changed; add the node again")
			}
			id := strings.TrimSpace(v["id"])
			if id == "" || strings.HasPrefix(id, "@") || strings.Contains(id, "/") {
				return fmt.Errorf("enter a node ID without / or a leading @")
			}
			d := m.editor.Clone()
			for _, s := range d.Steps {
				if s.ID == id {
					return fmt.Errorf("node ID already exists")
				}
			}
			d.Steps = append(d.Steps, workflow.Step{ID: id, Tool: n.Name, NodeVersion: n.Version, Args: json.RawMessage(`{}`)})
			m.editor.Replace(d)
			m.graph.Selected = id
			m.graph = m.buildGraph(m.graph)
			m.queueAction("edit", "")
			return nil
		},
	})
}

func (m *Model) edit() {
	if m.selectedSchedule() != nil {
		m.schedule()
		return
	}
	idx := -1
	for i, s := range m.editor.Definition.Steps {
		if s.ID == m.graph.Selected {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	s := m.editor.Definition.Steps[idx]
	n, err := m.registry.Node(s.Tool)
	if err != nil {
		m.dialogs.OpenDialog(screen.Dialog{Title: "Node", Lines: []string{err.Error()}})
		return
	}
	schema := schemaform.Parse(n.InputSchema)
	if m.inspectorOpen && m.inspect && m.inspectorTab == 0 {
		delete(schema.Properties, "code")
	}
	draft := m.editor.Source()
	m.dialogs.OpenForm(screen.Form{
		Title:      s.ID + " / " + s.Tool,
		Hint:       n.Description + " · * required. Use JSON objects for references.",
		ApplyLabel: "Apply",
		Fields:     schema.Fields(s.Args),
		Apply: func(v map[string]string) error {
			if m.run != nil || m.busy || m.editor.Source() != draft {
				return fmt.Errorf("draft changed; reopen node arguments")
			}
			args, err := schema.Values(s.Args, v)
			if err != nil {
				return err
			}
			d := m.editor.Clone()
			d.Steps[idx].Args = args
			m.editor.Replace(d)
			m.graph = m.buildGraph(m.graph)
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
