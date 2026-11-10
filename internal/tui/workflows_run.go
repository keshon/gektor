package tui

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

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/table"
	editor "github.com/keshon/gektor/internal/tui/workflow"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow/store"
)

type workflowQuit struct{}

func workflowWait(ch <-chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }
func (m *workflowScreen) action(a workflowAction) tea.Cmd {
	if m == nil {
		return nil
	}
	w := m
	if w.busy {
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
		if in.Draft != w.editor.Source() || w.run != nil {
			w.setNotice(pane.NoticeWarning, "Draft changed · reopen run inputs")
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
		w.runID = ""
		w.editor = editor.New()
		w.editor.Path = a.value
		w.graph = graph.Model{}
		w.run = nil
		return nil
	case "inspect-run":
		w.runID = a.value
		return m.poll(a.value, false)
	case "open-path", "save-path", "validate-inputs", "run-inputs", "review-approve", "review-reject":
	default:
		return m.command(a.action)
	}
	if w.busy {
		return nil
	}
	w.busy = true
	w.setNotice(pane.NoticeInfo, "Working…")
	source, path := w.editor.Source(), w.editor.Path
	ws, policy := m.config.WS, m.config.Policy
	if path == "" {
		path = "recipe.json"
	}
	ctx, cancel := context.WithCancel(m.ctx)
	w.cancel = cancel
	events := w.events
	runID := w.runID
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
			s.OnRun = func(id string) error { events <- workflowRunID(id); return nil }
			switch a.action {
			case "open-path":
				var b []byte
				b, err = s.Snapshot(ctx, a.value)
				if err == nil {
					_, err = editor.Decode(b)
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
		events <- result
	}()
	return workflowWait(events)
}

func (m *workflowScreen) openRuns(runs []store.RunSummary) {
	w := m
	if len(runs) == 0 {
		w.setNotice(pane.NoticeInfo, "No runs in this workspace")
		return
	}
	runs = append([]store.RunSummary(nil), runs...)
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].Started.After(runs[j].Started) })
	view := m.ui.newTable()
	rows := make([]table.Row, 0, len(runs))
	for _, run := range runs {
		started := "Unknown"
		if !run.Started.IsZero() {
			started = run.Started.Local().Format("2006-01-02 15:04")
		}
		rows = append(rows, table.Row{ID: run.ID, Cells: []string{cleanText(nonEmpty(run.Name)), cleanText(run.Status), started, run.ID, cleanText(run.RecipePath)}})
	}
	view.SetRows(rows)
	m.ui.openDialog("Workflow runs", nil)
	m.ui.dialog().table = &tableDialog{
		model:       view,
		columns:     []table.Column{{Label: "Recipe", Width: 14}, {Label: "Status", Width: 12}, {Label: "Started"}},
		description: "Saved runs in this workspace, newest first. Search by recipe, status, date or run ID. Open inspects a saved record; it does not run the recipe.",
		actions: func() []table.Action {
			return []table.Action{{ID: "details", Label: "Open run", Key: "enter", Enabled: view.Selected() != "" && m == w && !w.busy}, {ID: "close", Label: "Close", Key: "esc", Enabled: true}}
		},
		activate: func(action, id string) tea.Cmd {
			if action == "details" && id != "" && m == w && !w.busy {
				m.ui.closeDialog()
				m.queueAction("inspect-run", id)
			}
			return nil
		},
	}
}
func (m *workflowScreen) acceptPermission(p workflowPermission) tea.Cmd {
	w := m
	m.ui.openForm("Allow operation?", p.tool+" on "+p.resource, "Decide", []formField{{Key: "decision", Label: "Permission", Value: "Deny"}, {Key: "args", Label: "Resolved arguments", Value: prettyToolArguments(string(p.args)), Multiline: true, ReadOnly: true}}, func(v map[string]string) error {
		eff := permission.Deny
		if v["decision"] == "Allow once" {
			eff = permission.Allow
		}
		select {
		case p.reply <- eff:
		default:
		}
		return nil
	})
	m.ui.dialog().form.Choices = map[string][]string{"decision": {"Deny", "Allow once"}}
	m.ui.dialog().onClose = func() {
		select {
		case p.reply <- permission.Deny:
		default:
		}
	}
	return workflowWait(w.events)
}
func (m *workflowScreen) acceptResult(r workflowResult) tea.Cmd {
	w := m
	if w == nil {
		return nil
	}
	w.busy = false
	w.cancel = nil
	if m.ui.dialog() != nil && m.ui.dialog().onClose != nil {
		m.ui.closeDialog()
	}
	if r.err != nil {
		if r.action == "open-path" {
			w.setNotice(pane.NoticeWarning, "Recipe was not opened · Ctrl+O choose a file")
			m.command("open")
			if m.ui.dialog() != nil && m.ui.dialog().form != nil {
				m.ui.dialog().form.Editor.SetValue(r.path)
				message := "Could not open recipe: " + r.err.Error()
				if os.IsNotExist(r.err) {
					message = "Recipe not found: " + r.path + ". Enter an existing path, or Esc then Ctrl+N to create one."
				}
				m.ui.dialog().form.Error = message
			}
			return nil
		}
		w.setNotice(pane.NoticeError, r.err.Error())
		rows := m.validationRows(r.err)
		if r.text != "" {
			rows = append(rows, r.text)
		}
		m.ui.openDialog("Workflow failed", rows)
		return nil
	}
	if r.action == "schedule-save" || r.action == "schedule-delete" || r.action == "schedule-refresh" {
		w.schedules = r.schedules
		w.scheduleStatus, w.workerLeaseHeld = r.scheduleStatus, r.workerLeaseHeld
		w.graph = m.buildGraph(w.graph)
		m.loadLayout()
		if r.action == "schedule-save" {
			w.graph.Selected = workflowSchedulePrefix + r.scheduleID
			w.setNotice(pane.NoticeSuccess, "Schedule saved. Keep the workflow service running.")
		} else if r.action == "schedule-delete" {
			w.setNotice(pane.NoticeSuccess, "Schedule removed. Queued jobs and saved runs are kept.")
		} else {
			w.setNotice(pane.NoticeSuccess, "Schedules refreshed")
		}
		m.persistLayout()
		return nil
	}
	if r.source != "" {
		d, err := editor.Decode([]byte(r.source))
		if err != nil {
			w.setNotice(pane.NoticeError, err.Error())
			return nil
		}
		w.runID = ""
		w.editor = &editor.Editor{Definition: d, Path: r.path}
		w.editor.Saved = w.editor.Source()
		w.run = nil
		w.graph = m.buildGraph(graph.Model{})
		m.loadLayout()
	}
	if r.path != "" && r.source == "" {
		w.editor.Path = r.path
		w.editor.Saved = w.editor.Source()
		if err := m.saveLayout(); err != nil {
			w.setNotice(pane.NoticeError, "Recipe saved; layout: "+err.Error())
			return nil
		}
	}
	w.setNotice(pane.NoticeInfo, "Ready")
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
			w.setNotice(level, "Run: "+summary.Status+" / H opens saved runs")
		} else {
			w.setNotice(pane.NoticeInfo, r.text)
		}
	}
	if w.runID != "" {
		return m.poll(w.runID, false)
	}
	if r.action == "open-path" || r.action == "save-path" {
		return m.scheduleRefresh()
	}
	return nil
}
func (m *workflowScreen) poll(id string, delay bool) tea.Cmd {
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
func (m *workflowScreen) acceptPoll(p workflowPoll) tea.Cmd {
	w := m
	if w == nil || w.runID != p.id {
		return nil
	}
	if p.err != nil {
		w.setNotice(pane.NoticeError, p.err.Error())
		return nil
	}
	w.run = p.run
	e := &editor.Editor{Definition: m.definition()}
	w.graph = e.Graph(w.graph)
	for i := range w.graph.Nodes {
		for _, s := range p.run.Steps {
			if w.graph.Nodes[i].ID == s.ID {
				w.graph.Nodes[i].Detail = s.Status
			}
		}
	}
	if w.busy {
		return m.poll(p.id, true)
	}
	return nil
}
func (m *workflowScreen) layoutPath() string {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(m.config.WS.Root()+"\x00"+m.editor.Path)))
	return filepath.Join(userdirs.Sub("workflow-layouts"), key+".json")
}

type workflowLayout struct {
	Version   int                    `json:"version,omitempty"`
	Positions map[string]graph.Point `json:"positions"`
	Locked    bool                   `json:"locked"`
}

func (m *workflowScreen) saveLayout() error {
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
func (m *workflowScreen) loadLayout() {
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

func (m *workflowScreen) persistLayout() {
	if m.run != nil || m.editor.Path == "" {
		return
	}
	if err := m.saveLayout(); err != nil {
		m.setNotice(pane.NoticeError, "Could not save layout: "+err.Error())
	}
}
