package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/table"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
)

func (m *Model) wait() tea.Cmd {
	ctx, events := m.ctx, m.events
	return func() tea.Msg {
		select {
		case msg := <-events:
			return msg
		case <-ctx.Done():
			return nil
		}
	}
}
func (m *Model) action(a workflowAction) tea.Cmd {
	if m.ctx.Err() != nil {
		return nil
	}
	if m.busy {
		return nil
	}
	switch a.action {
	case "add-operation":
		m.addOperation(a.value)
		return nil
	case "schedule-save", "schedule-delete":
		return m.scheduleChange(a.action, a.value)
	case "input-form", "invoke":
		var in workflowInvocation
		if json.Unmarshal([]byte(a.value), &in) != nil {
			return nil
		}
		if in.Draft != m.editor.Source() || m.run != nil {
			m.setNotice(pane.NoticeWarning, "Draft changed · reopen run inputs")
			return nil
		}
		if a.action == "input-form" {
			m.inputForm(in)
			return nil
		}
		if in.Action == "schedule" {
			m.scheduleForm(in)
			return nil
		}
		if in.Action != "run" && in.Action != "validate" {
			return nil
		}
		return m.action(workflowAction{in.Action + "-inputs", string(in.Inputs)})
	case "parameter-edit":
		var edit workflowParameterEdit
		if json.Unmarshal([]byte(a.value), &edit) == nil {
			m.parameterEdit(edit)
		}
		return nil
	case "connection-edit":
		var edit workflowConnectionEdit
		if json.Unmarshal([]byte(a.value), &edit) == nil {
			m.connectionEdit(edit)
		}
		return nil
	case "link-target":
		m.linkTarget(a.value)
		return nil
	case "ai-prompt":
		return m.propose(a.value)
	case "new-path":
		m.runID = ""
		m.editor = NewEditor()
		m.editor.Path = a.value
		m.graph = graph.Model{}
		m.run = nil
		return nil
	case "inspect-run":
		m.runID = a.value
		return m.poll(a.value, false)
	case "open-path", "save-path", "validate-inputs", "run-inputs", "review-approve", "review-reject":
	default:
		return m.command(a.action)
	}
	if m.busy {
		return nil
	}
	m.busy = true
	m.setNotice(pane.NoticeInfo, "Working…")
	source, path := m.editor.Source(), m.editor.Path
	ws, policy := m.config.WS, m.config.Policy
	if path == "" {
		path = "recipe.json"
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	events := m.events
	screenCtx := m.ctx
	runID := m.runID
	go func() {
		defer cancel()
		gate := func(ctx context.Context, tool, resource string, args json.RawMessage) (permission.Effect, error) {
			reply := make(chan permission.Effect, 1)
			select {
			case events <- workflowPermission{tool, resource, args, reply}:
			case <-ctx.Done():
				return permission.Deny, ctx.Err()
			}
			select {
			case eff := <-reply:
				return eff, nil
			case <-ctx.Done():
				return permission.Deny, ctx.Err()
			}
		}
		s, err := app.NewWorkflow(ws, userdirs.Sub("workflows"), policy, gate)
		if err == nil {
			path, err = ws.Relative(path)
		}
		result := workflowResult{action: a.action}
		if err == nil {
			s.OnRun = func(id string) error {
				select {
				case events <- workflowRunID(id):
					return nil
				case <-screenCtx.Done():
					return screenCtx.Err()
				}
			}
			switch a.action {
			case "open-path":
				var b []byte
				b, err = s.Snapshot(ctx, a.value)
				if err == nil {
					_, err = Decode(b)
				}
				result.source, result.path = string(b), a.value
			case "save-path":
				result.text, err = s.Save(ctx, a.value, []byte(source))
				result.path = a.value
			case "run-inputs", "validate-inputs":
				s.ValidateOnly = a.action == "validate-inputs"
				var inputs map[string]json.RawMessage
				err = json.Unmarshal([]byte(a.value), &inputs)
				if err == nil && !s.ValidateOnly {
					err = permission.Authorize(ctx, policy, gate, "workflow_run", path, json.RawMessage(source))
				}
				if err == nil {
					result.text, err = s.RunSource(ctx, []byte(source), path, inputs)
				}
			case "review-approve", "review-reject":
				result.text, err = s.Review(ctx, runID, a.value, a.action == "review-approve")
			}
		}
		result.err = err
		select {
		case events <- result:
		case <-screenCtx.Done():
		}
	}()
	return m.wait()
}

func (m *Model) openRuns(runs []store.RunSummary) {
	if len(runs) == 0 {
		m.setNotice(pane.NoticeInfo, "No runs in this workspace")
		return
	}
	runs = append([]store.RunSummary(nil), runs...)
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].Started.After(runs[j].Started) })
	view := table.New(textinput.New().Styles())
	rows := make([]table.Row, 0, len(runs))
	for _, run := range runs {
		started := "Unknown"
		if !run.Started.IsZero() {
			started = run.Started.Local().Format("2006-01-02 15:04")
		}
		rows = append(rows, table.Row{ID: run.ID, Cells: []string{cleanText(nonEmpty(run.Name)), cleanText(run.Status), started, run.ID, cleanText(run.RecipePath)}})
	}
	view.SetRows(rows)
	m.dialogs.OpenDialog(screen.Dialog{Title: "Workflow runs", Lines: nil,
		Table: &screen.Table{
			Model:       view,
			Columns:     []table.Column{{Label: "Recipe", Width: 14}, {Label: "Status", Width: 12}, {Label: "Started"}},
			Description: "Saved runs in this workspace, newest first. Search by recipe, status, date or run ID. Open inspects a saved record; it does not run the recipe.",
			Actions: func() []table.Action {
				return []table.Action{{ID: "details", Label: "Open run", Key: "enter", Enabled: view.Selected() != "" && m.ctx.Err() == nil && !m.busy}, {ID: "close", Label: "Close", Key: "esc", Enabled: true}}
			},
			Activate: func(action, id string) tea.Cmd {
				if action == "details" && id != "" && m.ctx.Err() == nil && !m.busy {
					m.dialogs.CloseDialog()
					m.queueAction("inspect-run", id)
				}
				return nil
			},
		},
	})

}
func (m *Model) acceptPermission(p workflowPermission) tea.Cmd {
	m.dialogs.OpenForm(screen.Form{
		Title:      "Allow operation?",
		Hint:       p.tool + " on " + p.resource,
		ApplyLabel: "Decide",
		Fields:     []formField{{Key: "decision", Label: "Permission", Value: "Deny"}, {Key: "args", Label: "Resolved arguments", Value: prettyToolArguments(string(p.args)), Multiline: true, ReadOnly: true}},
		Apply: func(v map[string]string) error {
			eff := permission.Deny
			if v["decision"] == "Allow once" {
				eff = permission.Allow
			}
			select {
			case p.reply <- eff:
			default:
			}
			return nil
		},
		Choices: map[string][]string{"decision": {"Deny", "Allow once"}},
		OnClose: func() {
			select {
			case p.reply <- permission.Deny:
			default:
			}
		},
	})

	return m.wait()
}
func (m *Model) acceptResult(r workflowResult) tea.Cmd {
	if m.ctx.Err() != nil {
		return nil
	}
	m.busy = false
	m.cancel = nil
	m.dialogs.FinishOperation()
	if r.err != nil {
		if r.action == "open-path" {
			m.setNotice(pane.NoticeWarning, "Recipe was not opened · Ctrl+O choose a file")
			if m.active {
				form := m.recipePath("open")
				form.Editor.SetValue(r.path)
				message := "Could not open recipe: " + r.err.Error()
				if os.IsNotExist(r.err) {
					message = "Recipe not found: " + r.path + ". Enter an existing path, or Esc then Ctrl+N to create one."
				}
				form.Error = message
			}
			return nil
		}
		m.setNotice(pane.NoticeError, r.err.Error())
		rows := m.validationRows(r.err)
		if r.text != "" {
			rows = append(rows, r.text)
		}
		m.dialogs.OpenDialog(screen.Dialog{Title: "Workflow failed", Lines: rows})
		return nil
	}
	if r.action == "schedule-save" || r.action == "schedule-delete" || r.action == "schedule-refresh" {
		m.schedules = r.schedules
		m.scheduleStatus, m.workerLeaseHeld = r.scheduleStatus, r.workerLeaseHeld
		m.graph = m.buildGraph(m.graph)
		m.loadLayout()
		if r.action == "schedule-save" {
			m.graph.Selected = workflowSchedulePrefix + r.scheduleID
			m.setNotice(pane.NoticeSuccess, "Schedule saved. Keep the workflow service running.")
		} else if r.action == "schedule-delete" {
			m.setNotice(pane.NoticeSuccess, "Schedule removed. Queued jobs and saved runs are kept.")
		} else {
			m.setNotice(pane.NoticeSuccess, "Schedules refreshed")
		}
		m.persistLayout()
		return nil
	}
	if r.source != "" {
		d, err := Decode([]byte(r.source))
		if err != nil {
			m.setNotice(pane.NoticeError, err.Error())
			return nil
		}
		m.runID = ""
		m.editor = &Editor{Definition: d, Path: r.path}
		m.editor.Saved = m.editor.Source()
		m.run = nil
		m.graph = m.buildGraph(graph.Model{})
		m.loadLayout()
	}
	if r.path != "" && r.source == "" {
		m.editor.Path = r.path
		m.editor.Saved = m.editor.Source()
		if err := m.saveLayout(); err != nil {
			m.setNotice(pane.NoticeError, "Recipe saved; layout: "+err.Error())
			return nil
		}
	}
	m.setNotice(pane.NoticeInfo, "Ready")
	if r.text != "" {
		var summary struct {
			Status string `json:"status"`
		}
		if json.Unmarshal([]byte(r.text), &summary) == nil && summary.Status != "" {
			level := pane.NoticeInfo
			switch summary.Status {
			case "completed":
				level = pane.NoticeSuccess
			case "failed":
				level = pane.NoticeError
			case "cancelled":
				level = pane.NoticeWarning
			}
			m.setNotice(level, "Run: "+summary.Status+" / H opens saved runs")
		} else {
			m.setNotice(pane.NoticeInfo, r.text)
		}
	}
	if m.runID != "" {
		return m.poll(m.runID, false)
	}
	if r.action == "open-path" || r.action == "save-path" {
		return m.scheduleRefresh()
	}
	return nil
}
func (m *Model) poll(id string, delay bool) tea.Cmd {
	root := m.config.WS.Root()
	return func() tea.Msg {
		if delay {
			time.Sleep(250 * time.Millisecond)
		}
		s, err := store.OpenStore(userdirs.Sub("workflows"), root)
		if err != nil {
			return workflowPoll{id: id, err: err}
		}
		defer s.Close()
		r, err := s.Run(id)
		if err != nil {
			return workflowPoll{id: id, err: err}
		}
		r.Record.Status = r.Status
		return workflowPoll{id: id, run: &r.Record}
	}
}
func (m *Model) acceptPoll(p workflowPoll) tea.Cmd {
	if m.ctx.Err() != nil || m.runID != p.id {
		return nil
	}
	if p.err != nil {
		m.setNotice(pane.NoticeError, p.err.Error())
		return nil
	}
	m.run = p.run
	e := &Editor{Definition: m.definition()}
	m.graph = e.Graph(m.graph)
	for i := range m.graph.Nodes {
		for _, s := range p.run.Steps {
			if m.graph.Nodes[i].ID == s.ID {
				m.graph.Nodes[i].Detail = s.Status
			}
		}
	}
	if m.busy {
		return m.poll(p.id, true)
	}
	return nil
}
func (m *Model) layoutPath() string {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(m.config.WS.Root()+"\x00"+m.editor.Path)))
	return filepath.Join(userdirs.Sub("workflow-layouts"), key+".json")
}

type workflowLayout struct {
	Version   int                    `json:"version,omitempty"`
	Positions map[string]graph.Point `json:"positions"`
	Locked    bool                   `json:"locked"`
}

func (m *Model) saveLayout() error {
	p := m.layoutPath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	positions := map[string]graph.Point{}
	for _, n := range m.graph.Nodes {
		positions[n.ID] = n.Point
	}
	b, err := json.Marshal(workflowLayout{Version: 2, Positions: positions, Locked: m.positionsLocked})
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0600)
}
func (m *Model) loadLayout() {
	m.positionsLocked = false
	b, err := os.ReadFile(m.layoutPath())
	if err != nil {
		return
	}
	var layout workflowLayout
	if err := json.Unmarshal(b, &layout); err != nil || layout.Version != 2 || layout.Positions == nil {
		return
	}
	m.positionsLocked = layout.Locked
	hasTrigger := false
	for _, n := range m.graph.Nodes {
		if strings.HasPrefix(n.ID, workflowSchedulePrefix) {
			if _, ok := layout.Positions[n.ID]; ok {
				hasTrigger = true
			}
		}
	}
	for i, n := range m.graph.Nodes {
		if p, ok := layout.Positions[n.ID]; ok && p.X >= 0 && p.Y >= 0 && p.X < 10000 && p.Y < 10000 {

			m.graph.Nodes[i].Point = p
			if !hasTrigger && len(m.recipeSchedules()) > 0 && !strings.HasPrefix(n.ID, workflowSchedulePrefix) {
				m.graph.Nodes[i].X += graph.NodeWidth + 10
			}
		}
	}
}

func (m *Model) persistLayout() {
	if m.run != nil || m.editor.Path == "" {
		return
	}
	if err := m.saveLayout(); err != nil {
		m.setNotice(pane.NoticeError, "Could not save layout: "+err.Error())
	}
}

func (m *Model) service(gate permission.Gate) (service.Service, error) {
	return app.NewWorkflow(m.config.WS, userdirs.Sub("workflows"), m.config.Policy, gate)
}
