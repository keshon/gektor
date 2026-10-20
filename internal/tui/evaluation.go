package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/retrospect"
)

// A review owns only its dialog and context. It cannot replace the task's
// session, retry closure, history, token meter, or step counter.
type evaluationJob struct{ cancel context.CancelFunc }
type evaluationDoneMsg struct {
	job    *evaluationJob
	result retrospect.Result
	err    error
}

func (m *model) evaluate(arg string) (tea.Model, tea.Cmd) {
	if m.state != stDone || m.evaluation != nil {
		m.appendBlock(markerBlock("Finish the current run or evaluation first."))
		return m, nil
	}
	if m.stateFile == "" || m.ws == nil || m.baseEnv.Client == nil {
		m.appendBlock(errorBlock("Open a saved session with a configured model before /eval."))
		return m, nil
	}
	selection, focus, _ := strings.Cut(strings.TrimSpace(arg), " ")
	base := m.base
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	job := &evaluationJob{cancel: cancel}
	config := retrospect.Config{Client: m.baseEnv.Client, Workspace: m.ws, StateFile: m.stateFile, Scope: selection, Focus: strings.TrimSpace(focus), ContextLimit: m.limit, MaxTokens: m.baseEnv.MaxTokens}
	m.openDialog("Session evaluation", []string{"Reviewing the saved archive in a separate context…", "", "Keep this window open until the evaluation finishes.", "Closing it cancels the evaluation. The original task remains unchanged."})
	m.evaluation = job
	return m, func() tea.Msg {
		result, err := retrospect.Evaluate(ctx, config)
		return evaluationDoneMsg{job: job, result: result, err: err}
	}
}

func (m *model) cancelEvaluation() {
	if m.evaluation != nil {
		m.evaluation.cancel()
		m.evaluation = nil
	}
}

func (m *model) acceptEvaluation(msg evaluationDoneMsg) {
	if m.evaluation != msg.job {
		return
	}
	m.cancelEvaluation()
	m.closeDialog()
	if msg.err != nil {
		m.openDialog("Evaluation failed", []string{msg.err.Error()})
		return
	}
	r := msg.result
	rows := []string{r.Scope, fmt.Sprintf("Archive: messages %d–%d · inspected %d of %d", r.Start, r.End, r.Read, r.End-r.Start+1), "", "MODEL ASSESSMENT", r.Summary}
	if r.Partial > 0 {
		rows = append(rows, fmt.Sprintf("Coverage warning: %d inspected messages were only partially read.", r.Partial))
	}
	for _, finding := range r.Findings {
		rows = append(rows, "", fmt.Sprintf("%s [messages %v]", finding.Text, finding.Evidence))
	}
	if len(r.Memory) == 0 {
		rows = append(rows, "", "MEMORY", "No supported reusable observation.")
	} else {
		rows = append(rows, "", "MEMORY · historical observations")
		for _, memory := range r.Memory {
			rows = append(rows, "", memory.When, memory.Lesson, fmt.Sprintf("Evidence: messages %v · %s", memory.Evidence, memory.Quote))
		}
	}
	if r.Saved != "" {
		rows = append(rows, "", "Saved: "+r.Saved)
	}
	if r.Warning != "" {
		rows = append(rows, "", r.Warning)
	}
	m.openDialog("Session evaluation", rows)
}
