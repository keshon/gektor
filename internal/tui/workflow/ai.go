package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/authoring"
	"github.com/keshon/gektor/pkg/tui/components/pane"
	"github.com/keshon/gektor/pkg/tui/components/popup"
	"github.com/keshon/gektor/pkg/tui/screen"
)

type workflowProposal struct {
	before, source, note string
	err                  error
}

func (m *Model) propose(prompt string) tea.Cmd {
	if m.ctx.Err() != nil || m.busy {
		return nil
	}
	client := m.config.Client
	if client == nil {
		m.setNotice(pane.NoticeWarning, "Connect a model to request an AI proposal")
		return nil
	}
	s, err := m.service(nil)
	if err != nil {
		m.setNotice(pane.NoticeError, err.Error())
		return nil
	}
	before := m.editor.Source()
	request := authoring.Request{
		Source:           before,
		Change:           prompt,
		DefinitionSchema: s.DefinitionSchema(),
		Contracts:        m.registry.Contracts(),
		MaxTokens:        m.config.MaxTokens,
		ContextLimit:     m.config.ContextLimit,
		Temperature:      m.config.Temperature,
	}
	m.setNotice(pane.NoticeInfo, "AI drafting a proposal · Esc cancels")
	return m.startOperation(func(operationCtx context.Context) tea.Msg {
		proposal, requestErr := authoring.Propose(operationCtx, client, request)
		return workflowProposal{before: before, source: proposal.Source, note: proposal.Note, err: requestErr}
	})
}
func (m *Model) acceptProposal(p workflowProposal) {
	m.busy = false
	m.cancel = nil
	if p.err != nil {
		m.setNotice(pane.NoticeError, p.err.Error())
		return
	}
	s, err := m.service(nil)
	var d workflow.Definition
	if err == nil {
		d, err = authoring.Validate([]byte(p.source), s.DefinitionSchema())
	}
	if err != nil {
		m.dialogs.PresentDialog(screen.Dialog{Title: "Invalid AI proposal", Lines: []string{err.Error(), p.source}})
		return
	}
	lines := []string{"Proposal only · no files saved or operations executed.", ""}
	if p.note != "" {
		lines = append(lines, p.note, "")
	}
	old := map[string]string{}
	for _, step := range m.editor.Definition.Steps {
		b, _ := json.MarshalIndent(step, "", "  ")
		old[step.ID] = string(b)
	}
	for _, step := range d.Steps {
		b, _ := json.MarshalIndent(step, "", "  ")
		next := string(b)
		prev, ok := old[step.ID]
		if !ok {
			lines = append(lines, "+ Add "+step.ID, next)
		} else if prev != next {
			lines = append(lines, "~ Change "+step.ID, "Before:", prev, "After:", next)
		}
		delete(old, step.ID)
	}
	for id := range old {
		lines = append(lines, "- Remove "+id)
	}
	lines = append(lines, "", "Complete proposed recipe (including inputs/outputs):", p.source)
	// Split multiline sections so the shared scrollbar measures actual rows.
	m.dialogs.PresentDialog(screen.Dialog{Title: "Review AI proposal", Lines: strings.Split(strings.Join(lines, "\n"), "\n"),
		Shortcuts: []popup.Shortcut{{Key: "Esc/Enter", Action: "discard"}, {Key: "S", Action: "apply to draft"}, {Key: "↑↓", Action: "scroll"}},

		Actions: []screen.DialogAction{{ID: "apply-proposal", Key: 's', Label: "apply to draft"}},
	})
	m.proposalDialog = m.dialogs.DialogID()
	m.proposalDraft = d
	m.proposalBefore = p.before

}

func (m *Model) applyProposal() error {
	d := m.proposalDraft

	if m.busy || m.run != nil || m.editor.Source() != m.proposalBefore {
		return fmt.Errorf("draft changed since the request; ask for a fresh proposal")
	}
	m.editor.Replace(d)
	m.graph = m.buildGraph(m.graph)
	m.setNotice(pane.NoticeSuccess, "Proposal applied to draft · Ctrl+S saves")
	return nil
}
