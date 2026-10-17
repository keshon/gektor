package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/tars/internal/agent"
)

// Shared by help and command completion.
func commandReference() [][2]string {
	return [][2]string{
		{"/help", "Open this help"},
		{"/sessions", "Browse saved sessions"},
		{"/new [task]", "Start a fresh task; omit task for an empty session"},
		{"/mode plan|act", "Preview changes or execute them"},
		{"/max-steps N", "Step budget (0 = no limit)"},
		{"/budget-think N", "Reasoning budget in chars (0 = default)"},
		{"/budget-context N", "Context window in tokens (0 = default)"},
		{"/nudges full|recovery|off", "How much harness advice the model hears"},
		{"/status", "Show current run facts"},
		{"/retry", "Retry the last failed turn"},
		{"/compact", "Shrink this session's history"},
		{"/quit", "Quit"},
	}
}

// command handles local slash commands. Anything unrecognized is
// reported, never sent to the model — a typo must not become a task.
func (m *model) command(text string) (tea.Model, tea.Cmd) {
	name, arg, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
	m.input.Push(text)
	switch strings.ToLower(name) {
	case "q", "quit":
		m.quit = true
		m.cancel()
		return m, tea.Quit
	case "help":
		m.openHelp()
		return m, nil
	case "new":
		task := strings.TrimSpace(arg)
		if task == "" {
			// Bare /new resets to the empty state (same as
			// opening with no initial task): no session, clean
			// transcript, the next submitted line starts a fresh
			// run. The task text is the session title, so there is
			// nothing sensible to derive one from yet.
			m.newTask()
			return m, m.input.Focus()
		}
		clean, images, err := splitAttachments(m.ws, task)
		if err != nil {
			m.appendBlock(errorBlock(err.Error()))
			return m, nil
		}
		if err := m.startFreshPrompt(clean, task, images); err != nil {
			m.appendBlock(errorBlock("cannot start: " + err.Error()))
			return m, nil
		}
		return m, nil
	case "status":
		m.appendBlock(markerBlock(strings.Join(m.statusLines(), "\n")))
		return m, nil
	case "max-steps":
		n, err := strconv.Atoi(strings.TrimSpace(arg))
		if err != nil || n < 0 {
			m.appendBlock(errorBlock("Usage: /max-steps N (0 = no limit)"))
			return m, nil
		}
		m.maxStepsOverride = n
		if m.maxStepsOverride == 0 {
			m.maxStepsOverride = -1
		}
		if m.sess != nil {
			sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
			if err != nil {
				m.appendBlock(errorBlock(err.Error()))
				return m, nil
			}
			m.sess = sess
		}
		label := "no limit"
		if m.maxStepsOverride > 0 {
			label = strconv.Itoa(m.maxStepsOverride)
		}
		m.appendBlock(markerBlock("Max steps: " + label))
		return m, nil
	case "budget-think":
		n, err := strconv.Atoi(strings.TrimSpace(arg))
		if err != nil || n < 0 {
			m.appendBlock(errorBlock("Usage: /budget-think N (0 = default)"))
			return m, nil
		}
		m.thinkBudget = n
		if m.sess != nil {
			sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
			if err != nil {
				m.appendBlock(errorBlock(err.Error()))
				return m, nil
			}
			m.sess = sess
		}
		label := "default"
		if m.thinkBudget > 0 {
			label = strconv.Itoa(m.thinkBudget) + " chars"
		}
		m.appendBlock(markerBlock("Think budget: " + label))
		return m, nil
	case "budget-context":
		n, err := strconv.Atoi(strings.TrimSpace(arg))
		if err != nil || n < 0 {
			m.appendBlock(errorBlock("Usage: /budget-context N (0 = default)"))
			return m, nil
		}
		m.contextLimit = n
		if m.sess != nil {
			sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
			if err != nil {
				m.appendBlock(errorBlock(err.Error()))
				return m, nil
			}
			m.sess = sess
		}
		label := "default"
		if m.contextLimit > 0 {
			label = strconv.Itoa(m.contextLimit) + " tokens"
		}
		m.appendBlock(markerBlock("Context budget: " + label))
		return m, nil
	case "nudges":
		level := strings.ToLower(strings.TrimSpace(arg))
		if level == "" {
			m.appendBlock(markerBlock("Nudges: " + agent.NudgeLevelName(m.nudges) + " · /nudges full|recovery|off"))
			return m, nil
		}
		parsed, ok := agent.ParseNudgeLevel(level)
		if !ok {
			m.appendBlock(errorBlock("Choose /nudges full, recovery or off."))
			return m, nil
		}
		m.nudges = parsed
		if m.sess != nil {
			sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
			if err != nil {
				m.appendBlock(errorBlock(err.Error()))
				return m, nil
			}
			m.sess = sess
		}
		m.appendBlock(markerBlock("Nudges: " + agent.NudgeLevelName(m.nudges)))
		return m, nil
	case "mode":
		mode := strings.ToLower(strings.TrimSpace(arg))
		if mode == "" {
			m.appendBlock(markerBlock("Mode: " + m.modeName() + " · /mode plan previews changes · /mode act executes them"))
			return m, nil
		}
		if m.mission || (mode != "plan" && mode != "act") {
			m.appendBlock(errorBlock("Choose /mode plan or /mode act."))
			return m, nil
		}
		previous := m.plan
		m.plan = mode == "plan"
		if m.sess != nil {
			sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
			if err != nil {
				m.plan = previous
				m.appendBlock(errorBlock(err.Error()))
				return m, nil
			}
			if err := m.saveMode(); err != nil {
				m.plan = previous
				m.appendBlock(errorBlock(err.Error()))
				return m, nil
			}
			m.sess = sess
		}
		m.appendBlock(markerBlock("Mode: " + m.modeName()))
		return m, nil
	case "retry":
		if m.runErr == nil || m.retryRun == nil {
			m.appendBlock(markerBlock("nothing to retry: last run did not fail"))
			return m, nil
		}
		m.appendBlock(markerBlock("— retrying —"))
		m.retryRun()
		return m, nil
	case "sessions":
		m.openSessions()
		return m, nil
	case "compact":
		return m.compactNow()
	default:
		m.appendBlock(errorBlock("unknown command " + text + " (try /help)"))
		return m, nil
	}
}

func (m *model) commandReference() [][2]string {
	return commandReference()
}
