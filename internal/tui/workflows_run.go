package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	editor "github.com/keshon/gektor/internal/tui/workflow"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type workflowQuit struct{}

func workflowWait(ch <-chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }
func (m *model) workflowAction(a workflowAction) tea.Cmd {
	if m.workflows == nil {
		return nil
	}
	w := m.workflows
	if w.busy {
		return nil
	}
	switch a.action {
	case "add-operation":
		m.workflowAddOperation(a.value)
		return nil
	case "schedule-save", "schedule-delete":
		return m.workflowScheduleChange(a.action, a.value)
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
			m.workflowInputForm(in)
			return nil
		}
		if in.Action == "schedule" {
			m.workflowScheduleForm(in)
			return nil
		}
		if in.Action != "run" && in.Action != "validate" {
			return nil
		}
		return m.workflowAction(workflowAction{in.Action + "-inputs", string(in.Inputs)})
	case "parameter-edit":
		var edit workflowParameterEdit
		if json.Unmarshal([]byte(a.value), &edit) == nil {
			m.workflowParameterEdit(edit)
		}
		return nil
	case "connection-edit":
		var edit workflowConnectionEdit
		if json.Unmarshal([]byte(a.value), &edit) == nil {
			m.workflowConnectionEdit(edit)
		}
		return nil
	case "link-target":
		m.workflowLinkTarget(a.value)
		return nil
	case "ai-prompt":
		return m.proposeWorkflow(a.value)
	case "new-path":
		w.runID = ""
		w.editor = editor.New()
		w.editor.Path = a.value
		w.graph = graph.Model{}
		w.run = nil
		return nil
	case "inspect-run":
		w.runID = a.value
		return m.workflowPoll(a.value, false)
	case "open-path", "save-path", "validate-inputs", "run-inputs", "review-approve", "review-reject":
	default:
		return m.workflowCommand(a.action)
	}
	if w.busy {
		return nil
	}
	w.busy = true
	w.setNotice(pane.NoticeInfo, "Working…")
	source, path := w.editor.Source(), w.editor.Path
	ws, policy := m.baseEnv.WS, m.baseEnv.Policy
	if path == "" {
		path = "recipe.json"
	}
	ctx, cancel := context.WithCancel(m.base)
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
		s, err := app.NewAutomation(ws, gektor.Sub("workflows"), policy, gate)
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
func (m *model) acceptWorkflowPermission(p workflowPermission) tea.Cmd {
	w := m.workflows
	m.openForm("Allow operation?", p.tool+" on "+p.resource, "Decide", []formField{{Key: "decision", Label: "Permission", Value: "Deny"}, {Key: "args", Label: "Resolved arguments", Value: prettyToolArguments(string(p.args)), Multiline: true, ReadOnly: true}}, func(v map[string]string) error {
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
	m.dialog.form.Choices = map[string][]string{"decision": {"Deny", "Allow once"}}
	m.dialog.onClose = func() {
		select {
		case p.reply <- permission.Deny:
		default:
		}
	}
	return workflowWait(w.events)
}
func (m *model) acceptWorkflowResult(r workflowResult) tea.Cmd {
	w := m.workflows
	if w == nil {
		return nil
	}
	w.busy = false
	w.cancel = nil
	if m.dialog != nil && m.dialog.onClose != nil {
		m.closeDialog()
	}
	if r.err != nil {
		if r.action == "open-path" {
			w.setNotice(pane.NoticeWarning, "Recipe was not opened · Ctrl+O choose a file")
			m.workflowCommand("open")
			if m.dialog != nil && m.dialog.form != nil {
				m.dialog.form.Editor.SetValue(r.path)
				message := "Could not open recipe: " + r.err.Error()
				if os.IsNotExist(r.err) {
					message = "Recipe not found: " + r.path + ". Enter an existing path, or Esc then Ctrl+N to create one."
				}
				m.dialog.form.Error = message
			}
			return nil
		}
		w.setNotice(pane.NoticeError, r.err.Error())
		rows := m.workflowValidationRows(r.err)
		if r.text != "" {
			rows = append(rows, r.text)
		}
		m.openDialog("Workflow failed", rows)
		return nil
	}
	if r.action == "schedule-save" || r.action == "schedule-delete" || r.action == "schedule-refresh" {
		w.schedules = r.schedules
		w.graph = m.workflowGraph(w.graph)
		m.loadWorkflowLayout()
		if r.action == "schedule-save" {
			w.graph.Selected = workflowSchedulePrefix + r.scheduleID
			w.setNotice(pane.NoticeSuccess, "Schedule saved. Keep the workflow service running.")
		} else if r.action == "schedule-delete" {
			w.setNotice(pane.NoticeSuccess, "Schedule removed. Queued jobs and saved runs are kept.")
		} else {
			w.setNotice(pane.NoticeSuccess, "Schedules refreshed")
		}
		m.persistWorkflowLayout()
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
		w.graph = m.workflowGraph(graph.Model{})
		m.loadWorkflowLayout()
	}
	if r.path != "" && r.source == "" {
		w.editor.Path = r.path
		w.editor.Saved = w.editor.Source()
		if err := m.saveWorkflowLayout(); err != nil {
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
		return m.workflowPoll(w.runID, false)
	}
	if r.action == "open-path" || r.action == "save-path" {
		return m.workflowScheduleRefresh()
	}
	return nil
}
func (m *model) workflowPoll(id string, delay bool) tea.Cmd {
	root := m.wsRoot
	return func() tea.Msg {
		if delay {
			time.Sleep(250 * time.Millisecond)
		}
		s, err := store.OpenStore(gektor.Sub("workflows"), root)
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
func (m *model) acceptWorkflowPoll(p workflowPoll) tea.Cmd {
	w := m.workflows
	if w == nil || w.runID != p.id {
		return nil
	}
	if p.err != nil {
		w.setNotice(pane.NoticeError, p.err.Error())
		return nil
	}
	w.run = p.run
	e := &editor.Editor{Definition: m.workflowDefinition()}
	w.graph = e.Graph(w.graph)
	for i := range w.graph.Nodes {
		for _, s := range p.run.Steps {
			if w.graph.Nodes[i].ID == s.ID {
				w.graph.Nodes[i].Detail = s.Status
			}
		}
	}
	if w.busy {
		return m.workflowPoll(p.id, true)
	}
	return nil
}
func (m *model) workflowLayoutPath() string {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(m.wsRoot+"\x00"+m.workflows.editor.Path)))
	return filepath.Join(gektor.Sub("workflow-layouts"), key+".json")
}

type workflowLayout struct {
	Version   int                    `json:"version,omitempty"`
	Positions map[string]graph.Point `json:"positions"`
	Locked    bool                   `json:"locked"`
}

func (m *model) saveWorkflowLayout() error {
	p := m.workflowLayoutPath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	positions := map[string]graph.Point{}
	for _, n := range m.workflows.graph.Nodes {
		positions[n.ID] = n.Point
	}
	b, err := json.Marshal(workflowLayout{Version: 2, Positions: positions, Locked: m.workflows.positionsLocked})
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0600)
}
func (m *model) loadWorkflowLayout() {
	m.workflows.positionsLocked = false
	b, err := os.ReadFile(m.workflowLayoutPath())
	if err != nil {
		return
	}
	var layout workflowLayout
	if err := json.Unmarshal(b, &layout); err != nil || layout.Positions == nil { // Older files contained only the position map.
		layout.Locked = false
		if json.Unmarshal(b, &layout.Positions) != nil {
			return
		}
	}
	m.workflows.positionsLocked = layout.Locked
	hasTrigger := false
	for _, n := range m.workflows.graph.Nodes {
		if strings.HasPrefix(n.ID, workflowSchedulePrefix) {
			if _, ok := layout.Positions[n.ID]; ok {
				hasTrigger = true
			}
		}
	}
	for i, n := range m.workflows.graph.Nodes {
		if p, ok := layout.Positions[n.ID]; ok && p.X >= 0 && p.Y >= 0 && p.X < 10000 && p.Y < 10000 {
			// Earlier cards used 30x8 layout spacing. Make room for named ports
			// while retaining the relative arrangement of existing user layouts.
			if layout.Version < 2 {
				if p.X >= 2 {
					p.X = 2 + (p.X-2)*(graph.NodeWidth+10)/30
				}
				if p.Y >= 1 {
					p.Y = 1 + (p.Y-1)*14/8
				}
			}
			m.workflows.graph.Nodes[i].Point = p
			if !hasTrigger && len(m.workflowRecipeSchedules()) > 0 && !strings.HasPrefix(n.ID, workflowSchedulePrefix) {
				m.workflows.graph.Nodes[i].X += graph.NodeWidth + 10
			}
		}
	}
}

func (m *model) persistWorkflowLayout() {
	if m.workflows.run != nil || m.workflows.editor.Path == "" {
		return
	}
	if err := m.saveWorkflowLayout(); err != nil {
		m.workflows.setNotice(pane.NoticeError, "Could not save layout: "+err.Error())
	}
}
