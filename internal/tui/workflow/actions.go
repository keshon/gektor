package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
	"github.com/keshon/gektor/pkg/tui/components/graph"
	"github.com/keshon/gektor/pkg/tui/components/pane"
	"github.com/keshon/gektor/pkg/tui/components/popup"
	"github.com/keshon/gektor/pkg/tui/components/schemaform"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func (m *Model) command(action string) tea.Cmd {
	if !m.active {
		return nil
	}
	if action == "access-ask" || action == "access-full" {
		if !m.busy {
			m.approvals.SetFullAccess(action == "access-full")
		}
		return nil
	}
	if action == "inspector" {
		m.inspectorOpen = !m.inspectorOpen
		m.inspect = m.inspectorOpen
		m.thumb = popup.ScrollDrag{}
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
		m.setNotice(pane.NoticeInfo, "Operation in progress"+screen.Separator()+"Esc stops it")
		return nil
	}
	if m.run != nil && workflowRequiresDraft(action) {
		m.setNotice(pane.NoticeInfo, "Frozen run"+screen.Separator()+"D returns to the editable draft")
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
		m.dialogs.PresentDialog(screen.Dialog{Title: "Workflow", Lines: []string{err.Error()}})
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
		m.recipePath(action, "")
	case "save":
		m.saveForm(m.editor.Path)
	case "add":
		return m.addNodeForm()

	case "edit":
		m.edit()
	case "behavior":
		return m.behaviorForm()
	case "binding":
		m.savedConnectionForm()
	case "bind-input":
		m.bindRecipeInput()

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
	case "edit", "add", "link", "connections", "delete", "recipe", "name", "inputs", "outputs", "ai", "behavior", "binding", "bind-input", "undo", "redo", "save", "run", "validate":
		return true
	default:
		return false
	}
}

func (m *Model) recipePath(action, path string) {
	if path == "" {
		path = m.editor.Path
	}
	fields := []formField{{Key: "path", Label: "Recipe path (relative to workspace)", Value: path}}
	var fileFields map[string]fieldform.FilePicker
	if action == "open" {
		fileFields = map[string]fieldform.FilePicker{"path": {Root: m.config.WS.Root(), Extensions: []string{".json"}}}
	}
	if action == "new" {
		fields[0].Value = "recipe.json"
	}
	if m.editor.Dirty() {
		fields = append(fields, formField{Key: "discard", Label: "Unsaved changes: type discard to replace"})
	}
	m.openForm(screen.Form{
		Title:      "Workflow / " + action,
		Hint:       "Workspace: " + m.config.WS.Root(),
		ApplyLabel: "Continue",
		Fields:     fields,
		FileFields: fileFields,
	}, func(v map[string]string) error {
		if m.editor.Dirty() && v["discard"] != "discard" {
			return fmt.Errorf("save first, or type discard")
		}
		if strings.TrimSpace(v["path"]) == "" {
			return fmt.Errorf("enter a recipe path")
		}
		path, err := m.config.WS.Relative(strings.TrimSpace(v["path"]))
		if err != nil {
			return err
		}
		m.queueAction(action+"-path", path)
		return nil
	})
}

func (m *Model) saveForm(path string) {
	m.openForm(screen.Form{
		Title:      "Save workflow",
		Hint:       "Writes the recipe; canvas positions stay in your user settings.",
		ApplyLabel: "Save",
		Fields:     []formField{{Key: "path", Label: "Recipe path", Value: path}},
	}, func(v map[string]string) error {
		if strings.TrimSpace(v["path"]) == "" {
			return fmt.Errorf("enter a path")
		}
		path, err := m.config.WS.Relative(strings.TrimSpace(v["path"]))
		if err != nil {
			return err
		}
		file, err := m.config.WS.Lstat(path)
		if os.IsNotExist(err) {
			m.queueAction("save-path", path)
			return nil
		}
		if err != nil {
			return err
		}
		if !file.Mode().IsRegular() {
			return fmt.Errorf("choose a regular recipe file or a new path")
		}
		if path == m.editor.savedPath && file.Size() <= 64<<10 {
			raw, err := m.config.WS.ReadFile(path)
			if err != nil {
				return err
			}
			original, err := Decode(raw)
			if err == nil && (&Editor{Definition: original}).Source() == m.editor.Saved {
				m.queueAction("replace-path", path)
				return nil
			}
		}
		m.queueAction("confirm-replace", path)
		return nil
	})
}

func (m *Model) confirmReplace(path string) {
	draft := m.editor.Source()
	m.openForm(screen.Form{
		Title:         "Replace existing file?",
		Hint:          "This replaces the contents of the selected workspace file.",
		Fields:        []formField{{Key: "path", Label: "Existing file", Value: path, ReadOnly: true}},
		ApplyLabel:    "Replace",
		Destructive:   true,
		StartOnCancel: true,
	}, func(_ map[string]string) error {
		if m.editor.Source() != draft {
			return fmt.Errorf("draft changed; save again")
		}
		m.queueAction("replace-path", path)
		return nil
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
	schema := schemaform.Parse(n.InputSchema)
	empty := json.RawMessage(`{}`)
	choices := schema.Choices()
	fields := []formField{{Key: "$id", Label: "Node ID", Value: m.nextNodeID()}}
	fields = append(fields, schema.Fields(empty)...)
	draft := m.editor.Source()
	m.openForm(screen.Form{
		Title:      "Add " + name,
		Hint:       n.Description + screen.Separator() + "Fill arguments now or connect values later.",
		ApplyLabel: "Add node",
		Fields:     fields,
		Choices:    choices,
	}, func(v map[string]string) error {
		if m.busy || m.run != nil || m.editor.Source() != draft {
			return fmt.Errorf("draft changed; add the node again")
		}

		id := strings.TrimSpace(v["$id"])
		if id == "" || strings.HasPrefix(id, "@") || strings.Contains(id, "/") {
			return fmt.Errorf("enter a node ID without / or a leading @")
		}

		d := m.editor.Clone()
		for _, s := range d.Steps {
			if s.ID == id {
				return fmt.Errorf("node ID already exists")
			}
		}

		draftSchema := schema
		draftSchema.Required = nil
		args, err := draftSchema.Values(empty, v)
		if err != nil {
			return err
		}
		d.Steps = append(d.Steps, workflow.Step{ID: id, Tool: n.Name, NodeVersion: n.Version, Args: args})
		m.editor.Replace(d)
		m.graph.Selected = id
		m.graph = m.buildGraph(m.graph)
		m.setNotice(pane.NoticeSuccess, "Node added. Enter edits arguments; C connects nodes.")
		return nil
	})
}

func (m *Model) nextNodeID() string {
	ids := make(map[string]bool, len(m.editor.Definition.Steps))
	for _, step := range m.editor.Definition.Steps {
		ids[step.ID] = true
	}
	for i := len(ids) + 1; ; i++ {
		id := "step_" + strconv.Itoa(i)
		if !ids[id] {
			return id
		}
	}
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
		m.dialogs.PresentDialog(screen.Dialog{Title: "Node", Lines: []string{err.Error()}})
		return
	}
	schema := schemaform.Parse(n.InputSchema)
	if m.inspectorOpen && m.inspect && m.inspectorTab == 0 {
		delete(schema.Properties, "code")
	}
	draft := m.editor.Source()
	m.openForm(screen.Form{
		Title:      s.ID + " / " + s.Tool,
		Hint:       n.Description + screen.Separator() + "* required. Use JSON objects for references.",
		ApplyLabel: "Apply",
		Fields:     schema.Fields(s.Args),
		Choices:    schema.Choices(),
	}, func(v map[string]string) error {
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

func (m *Model) footerActions() []screen.Action {
	edit := !m.busy && m.run == nil
	hasNode := m.graph.Selected != ""
	hasSteps := len(m.definition().Steps) > 0
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
		{Key: tea.KeyEnter, Name: "Enter", Label: label, Short: label, Enabled: !m.busy && hasNode},
		{Key: 'c', Name: "C", Label: "Connect", Short: "Link", Enabled: edit && hasSteps && hasNode},
		{Key: 'r', Name: "R", Label: "Run", Short: "Run", Enabled: edit && hasSteps},
		{Key: 't', Mod: tea.ModCtrl, Name: "Ctrl+T", Label: "Inspector", Short: "Inspect", Enabled: hasNode},
		{Key: 's', Mod: tea.ModCtrl, Name: "Ctrl+S", Label: "Save", Short: "Save", Enabled: edit},
		{Key: tea.KeyEscape, Name: "Esc", Label: back, Short: back, Enabled: true},
	}
	if m.surface.Area.Width < 80 {
		return []screen.Action{actions[0], actions[1], actions[5], actions[7]}
	}
	return actions
}
