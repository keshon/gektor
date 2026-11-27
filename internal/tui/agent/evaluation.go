package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/retrospective"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/components/popup"
	"github.com/keshon/gektor/pkg/tui/screen"
)

// Only an unquoted selector consumes the first word; the rest is plain prose.
func reviewArguments(arg string) (selection, focus string) {
	arg = strings.TrimSpace(arg)
	first, rest := arg, ""
	if i := strings.IndexFunc(arg, unicode.IsSpace); i >= 0 {
		first, rest = arg[:i], strings.TrimSpace(arg[i:])
	}
	_, numberErr := strconv.Atoi(first)
	if first == "last" || first == "session" || numberErr == nil {
		selection, arg = first, rest
	}
	if len(arg) >= 2 && (arg[0] == '"' || arg[0] == '\'') && arg[len(arg)-1] == arg[0] && !strings.ContainsRune(arg[1:len(arg)-1], rune(arg[0])) {
		arg = arg[1 : len(arg)-1]
	}
	return selection, arg
}

// A review owns only its dialog and context. It cannot replace the task's
// session, retry closure, history, token meter, or step counter.
type evaluationJob struct {
	dialog   screen.DialogID
	learn    bool
	cancel   context.CancelFunc
	started  time.Time
	mu       sync.Mutex
	progress retrospective.Progress
}

func (j *evaluationJob) update(progress retrospective.Progress) {
	j.mu.Lock()
	j.progress = progress
	j.mu.Unlock()
}

func (m *Model) refreshEvaluation() {
	j := m.evaluation
	if j == nil || m.chrome.DialogID() != j.dialog {
		return
	}
	j.mu.Lock()
	p := j.progress
	j.mu.Unlock()
	m.chrome.UpdateDialog(j.dialog, "Session evaluation", []string{
		"Scope: " + p.Scope,
		p.Request,
		p.Stage,
		fmt.Sprintf("Step %d/%d"+screen.Separator()+"elapsed %s", p.Step, p.Steps, formatElapsed(time.Since(j.started))),
		fmt.Sprintf("Archive: inspected %d of %d messages (excerpts may be partial)", p.Read, p.Total),
		"", "Keep this window open until the evaluation finishes.",
		"Closing it cancels the evaluation. The original task remains unchanged.",
	})
}

type evaluationDoneMsg struct {
	job    *evaluationJob
	result retrospective.Result
	err    error
}

func (m *Model) evaluate(arg string) (tea.Model, tea.Cmd) {
	if m.state != stDone || m.evaluation != nil {
		m.appendBlock(markerBlock("Finish the current run or evaluation first."))
		return m, nil
	}
	if m.stateFile == "" || m.ws == nil || m.baseEnv.Client == nil {
		m.appendBlock(errorBlock("Open a saved session with a configured model before /eval."))
		return m, nil
	}
	selection, focus := reviewArguments(arg)
	m.cancelSessionTitle()
	base := m.base
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	job := &evaluationJob{cancel: cancel, started: time.Now(), progress: retrospective.Progress{Stage: "Loading saved archive", Steps: 12}}
	config := retrospective.Config{Client: m.baseEnv.Client, Workspace: m.ws, StateFile: m.stateFile, Scope: selection, Focus: strings.TrimSpace(focus), ContextLimit: m.limit, MaxTokens: m.baseEnv.MaxTokens}
	m.chrome.PresentDialog(screen.Dialog{Title: "Session evaluation", Lines: []string{"Reviewing the saved archive in a separate context…", "", "Keep this window open until the evaluation finishes.", "Closing it cancels the evaluation. The original task remains unchanged."}, Shortcuts: []popup.Shortcut{{Key: "Esc/Enter", Action: "cancel evaluation"}}})
	job.dialog = m.chrome.DialogID()
	m.evaluation = job
	config.OnProgress = job.update
	m.refreshEvaluation()
	return m, m.workerCommand(func() tea.Msg {
		defer cancel()
		result, err := retrospective.Evaluate(ctx, config)
		return evaluationDoneMsg{job: job, result: result, err: err}
	})
}

func (m *Model) cancelEvaluation() {
	if m.evaluation != nil {
		m.evaluation.cancel()
		m.evaluation = nil
	}
}

func (m *Model) acceptEvaluation(msg evaluationDoneMsg) {
	if m.evaluation != msg.job || m.chrome.DialogID() != msg.job.dialog {
		return
	}
	m.cancelEvaluation()
	m.chrome.Close()
	if msg.err != nil {
		m.chrome.Open("Evaluation failed", []string{msg.err.Error()})
		return
	}
	r := msg.result
	if msg.job.learn && len(r.Memory) == 1 {
		m.reviewLesson(r)
		return
	}
	if msg.job.learn && len(r.Memory) == 0 {
		m.chrome.Open("No lesson to save", []string{"Nothing reusable was supported by this review. No project files changed.", "", r.Summary})
		return
	}
	rows := []string{r.Scope, fmt.Sprintf("Archive: messages %d–%d"+screen.Separator()+"inspected %d of %d", r.Start, r.End, r.Read, r.End-r.Start+1), "", "MODEL ASSESSMENT", r.Summary}
	if r.Focus != "" {
		rows = append([]string{"YOUR FOCUS", r.Focus, ""}, rows...)
	}
	if r.Partial > 0 {
		rows = append(rows, fmt.Sprintf("Coverage warning: %d inspected messages were only partially read.", r.Partial))
	}
	for _, finding := range r.Findings {
		rows = append(rows, "", fmt.Sprintf("%s [messages %v]", finding.Text, finding.Evidence))
	}
	if len(r.Memory) == 0 {
		rows = append(rows, "", "MEMORY", "No supported reusable observation.")
	} else {
		rows = append(rows, "", "PROPOSED LESSON"+screen.Separator()+"not saved")
		for _, memory := range r.Memory {
			rows = append(rows, "", memory.When, memory.Lesson, fmt.Sprintf("Evidence: messages %v"+screen.Separator()+"%s", memory.Evidence, memory.Quote))
		}
	}
	if r.Saved != "" {
		rows = append(rows, "", "Saved: "+r.Saved)
	}
	if r.Warning != "" {
		rows = append(rows, "", r.Warning)
	}
	dialog := screen.Dialog{Title: "Session evaluation", Lines: rows}
	if len(r.Memory) == 1 {
		dialog.Actions = []screen.DialogAction{{ID: "review-lesson", Key: 'r', Label: "review lesson"}}
	}
	m.chrome.PresentDialog(dialog)
	if len(r.Memory) == 1 {
		if m.lessonDialogs == nil {
			m.lessonDialogs = make(map[screen.DialogID]retrospective.Result)
		}
		m.lessonDialogs[m.chrome.DialogID()] = r
	}
}

func (m *Model) learn(arg string) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(arg) == "review" {
		if m.state != stDone || m.evaluation != nil {
			m.appendBlock(markerBlock("Finish the current run or evaluation first."))
			return m, nil
		}
		if m.ws == nil || m.stateFile == "" {
			m.appendBlock(markerBlock("Open a saved session before reviewing a lesson."))
			return m, nil
		}
		failureData, failureErr := os.ReadFile(filepath.Join(filepath.Dir(m.stateFile), "evaluation-failure.json"))
		if failureErr == nil {
			var failure retrospective.Failure
			if err := json.Unmarshal(failureData, &failure); err != nil {
				m.chrome.Open("Cannot read review diagnostics", []string{err.Error()})
			} else {
				m.chrome.Open("Last review failed", strings.Split(failure.Error(), "\n"))
			}
			return m, nil
		} else if !os.IsNotExist(failureErr) {
			m.chrome.Open("Cannot read review diagnostics", []string{failureErr.Error()})
			return m, nil
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(m.stateFile), "evaluation.json"))
		var r retrospective.Result
		if err == nil {
			err = json.Unmarshal(data, &r)
		}
		if err != nil {
			m.chrome.Open("No saved review", []string{"Run /learn to review a completed task first.", err.Error()})
		} else if len(r.Memory) != 1 {
			m.chrome.Open("No lesson to save", []string{"The saved review has no single reusable lesson. Run /learn for a fresh review."})
		} else {
			m.reviewLesson(r)
		}
		return m, nil
	}
	if strings.TrimSpace(arg) == "undo" {
		if m.state != stDone || m.evaluation != nil {
			m.appendBlock(markerBlock("Finish the current run or evaluation first."))
			return m, nil
		}
		if m.ws == nil || m.lessonRoot != m.ws.Root() {
			m.appendBlock(markerBlock("No newly saved lesson to undo in this workspace."))
			return m, nil
		}
		if err := retrospective.Undo(m.lessonRoot, m.lastLesson); err != nil {
			m.appendBlock(errorBlock(err.Error()))
			return m, nil
		}
		m.appendBlock(markerBlock("Lesson disabled for future discovery. Evidence retained. Start a new session if it was already loaded into this conversation."))
		m.lastLesson = retrospective.Lesson{}
		return m, nil
	}
	next, cmd := m.evaluate(arg)
	if cmd != nil && m.evaluation != nil {
		m.evaluation.learn = true
	}
	return next, cmd
}

func (m *Model) reviewLesson(r retrospective.Result) {
	if m.chrome.DialogID() != (screen.DialogID{}) {
		m.chrome.Close()
	}
	memory := r.Memory[0]
	rows := []string{"WHEN", memory.When, "", "PROCEDURE", memory.Lesson, "", "EVIDENCE", fmt.Sprintf("Messages %v: %s", memory.Evidence, memory.Quote), "", "Historical evidence, not independent verification. Save only if useful for future tasks in this repository. The quoted evidence is saved with the lesson."}
	if r.Focus != "" {
		rows = append([]string{"YOUR FOCUS", r.Focus, ""}, rows...)
	}
	rows = append(rows, "Reopen this proposal later with /learn review, without another model call.")
	if r.Partial > 0 {
		rows = append(rows, "Some archive messages were only partially read.")
	}
	m.chrome.PresentDialog(screen.Dialog{
		Title: "Remember a repository lesson", Lines: rows,
		Actions:   []screen.DialogAction{{ID: "save-lesson", Key: 's', Label: "save lesson"}, {ID: "edit-lesson", Key: 'e', Label: "edit"}},
		Shortcuts: []popup.Shortcut{{Key: "Esc/Enter", Action: "skip"}, {Key: "S", Action: "save lesson"}, {Key: "E", Action: "edit"}},
	})
	if m.lessonDialogs == nil {
		m.lessonDialogs = make(map[screen.DialogID]retrospective.Result)
	}
	m.lessonDialogs[m.chrome.DialogID()] = r
}

func (m *Model) editLesson(r retrospective.Result) {
	if m.chrome.DialogID() != (screen.DialogID{}) {
		m.chrome.Close()
	}
	memory := r.Memory[0]
	m.openForm(screen.Form{Title: "Edit repository lesson", Hint: "Save for this repository. Historical guidance; check current files before use.", ApplyLabel: "Save lesson", Fields: []form.Field{
		{Key: "when", Label: "When to use", Value: memory.When},
		{Key: "procedure", Label: "Procedure", Value: memory.Lesson, Multiline: true},
	}}, func(values map[string]string) error {
		return m.saveLesson(r, values["when"], values["procedure"])
	})
}

func (m *Model) saveLesson(r retrospective.Result, when, procedure string) error {
	root, stateFile := m.ws.Root(), m.stateFile
	ctx := m.base
	if ctx == nil {
		ctx = context.Background()
	}
	lesson, err := retrospective.Approve(ctx, root, stateFile, r, when, procedure)
	if err != nil {
		return err
	}
	if lesson.Created {
		m.lastLesson, m.lessonRoot = lesson, root
		m.appendBlock(markerBlock("Lesson saved: " + filepath.Join(root, lesson.Path) + screen.Separator() + "Available through skill discovery on your next task. /learn undo disables it. Review before sharing in Git."))
	} else {
		m.appendBlock(markerBlock("This lesson is already saved; no duplicate created."))
	}
	return nil
}
