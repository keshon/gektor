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
	editor "github.com/keshon/gektor/internal/tui/workflow"
	"os"
	"path/filepath"
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
	w.notice = "Working…"
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
		result := workflowResult{}
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
		w.notice = r.err.Error()
		m.openDialog("Workflow failed", []string{r.err.Error(), r.text})
		return nil
	}
	if r.source != "" {
		d, err := editor.Decode([]byte(r.source))
		if err != nil {
			w.notice = err.Error()
			return nil
		}
		w.runID = ""
		w.editor = &editor.Editor{Definition: d, Path: r.path}
		w.editor.Saved = w.editor.Source()
		w.run = nil
		w.graph = w.editor.Graph(graph.Model{})
		m.loadWorkflowLayout()
	}
	if r.path != "" && r.source == "" {
		w.editor.Path = r.path
		w.editor.Saved = w.editor.Source()
		if err := m.saveWorkflowLayout(); err != nil {
			w.notice = "Recipe saved; layout: " + err.Error()
			return nil
		}
	}
	w.notice = "Ready"
	if r.text != "" {
		w.notice = r.text
	}
	if w.runID != "" {
		return m.workflowPoll(w.runID, false)
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
		w.notice = p.err.Error()
		return nil
	}
	w.run = p.run
	e := &editor.Editor{Definition: p.run.Definition}
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
func (m *model) saveWorkflowLayout() error {
	p := m.workflowLayoutPath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	positions := map[string]graph.Point{}
	for _, n := range m.workflows.graph.Nodes {
		positions[n.ID] = n.Point
	}
	b, err := json.Marshal(positions)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0600)
}
func (m *model) loadWorkflowLayout() {
	b, err := os.ReadFile(m.workflowLayoutPath())
	if err != nil {
		return
	}
	var positions map[string]graph.Point
	if json.Unmarshal(b, &positions) != nil {
		return
	}
	for i, n := range m.workflows.graph.Nodes {
		if p, ok := positions[n.ID]; ok && p.X >= 0 && p.Y >= 0 && p.X < 10000 && p.Y < 10000 {
			m.workflows.graph.Nodes[i].Point = p
		}
	}
}

func (m *model) persistWorkflowLayout() {
	if m.workflows.run != nil || m.workflows.editor.Path == "" {
		return
	}
	if err := m.saveWorkflowLayout(); err != nil {
		m.workflows.notice = "Could not save layout: " + err.Error()
	}
}
