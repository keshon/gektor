package workflow

import (
	"encoding/json"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.ctx.Err() != nil {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	case workflowProposal:
		m.acceptProposal(msg)
	case workflowAction:
		return m, m.action(msg)
	case workflowPermission:
		return m, m.acceptPermission(msg)
	case workflowResult:
		return m, m.acceptResult(msg)
	case workflowRunID:
		m.runID = string(msg)
		return m, tea.Batch(m.wait(), m.poll(string(msg), false))
	case workflowPoll:
		return m, m.acceptPoll(msg)
	}
	return m, nil
}

type workflowAction struct{ action, value string }

type workflowResult struct {
	text            string
	err             error
	source, path    string
	action          string
	schedules       []store.Schedule
	scheduleStatus  []service.ScheduleInspection
	workerLeaseHeld bool
	scheduleID      string
}

type workflowPermission struct {
	tool, resource string
	args           json.RawMessage
	reply          chan permission.Effect
}

type workflowRunID string

type workflowPoll struct {
	id  string
	run *workflow.Run
	err error
}

func (m *Model) queueAction(action, value string) {
	m.commands = append(m.commands, func() tea.Msg { return workflowAction{action, value} })
}
