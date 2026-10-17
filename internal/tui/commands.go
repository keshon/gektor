package tui

import (
	"fmt"
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
		{"/sidebar", "Toggle sidebar"},
		{"/details", "Toggle reply truncation"},
		{"/sys", "Toggle SYS messages"},
		{"/labels", "Toggle labels"},
		{"/latest", "Jump to latest"},
		{"/stop", "Stop the running task"},
		{"/rename [title]", "Rename the current session"},
		{"/delete", "Delete the current session"},
		{"/permissions", "Show permission policy"},
		{"/model", "Show model and backend"},
		{"/context", "Show context usage"},
		{"/run", "Show run stats"},
		{"/activity", "Show run activity"},
		{"/about", "About"},
		{"/theme dark|light", "Switch theme"},
		{"/settings", "Open settings"},
		{"/status", "Show current run facts"},
		{"/retry", "Retry the last failed turn"},
		{"/compact", "Shrink this session's history"},
		{"/quit", "Quit"},
	}
}

// confirmRestart asks before restarting a running session. If no session
// is running, the action runs immediately.
func (m *model) confirmRestart(action func()) {
	if m.sess == nil {
		action()
		return
	}
	m.confirmAction = action
	m.openModal(
		"",
		"Restart session?",
		"This command changes agent settings.",
		[]string{"The running session will restart."},
		"y  restart   n  cancel",
		"Esc close",
	)
}

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
		m.confirmRestart(func() {
			m.maxStepsOverride = n
			if m.maxStepsOverride == 0 {
				m.maxStepsOverride = -1
			}
			if m.sess != nil {
				sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
				if err != nil {
					m.appendBlock(errorBlock(err.Error()))
					return
				}
				m.sess = sess
			}
			label := "no limit"
			if m.maxStepsOverride > 0 {
				label = strconv.Itoa(m.maxStepsOverride)
			}
			m.appendBlock(markerBlock("Max steps: " + label))
		})
		return m, nil
	case "budget-think":
		n, err := strconv.Atoi(strings.TrimSpace(arg))
		if err != nil || n < 0 {
			m.appendBlock(errorBlock("Usage: /budget-think N (0 = default)"))
			return m, nil
		}
		m.confirmRestart(func() {
			m.thinkBudget = n
			if m.sess != nil {
				sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
				if err != nil {
					m.appendBlock(errorBlock(err.Error()))
					return
				}
				m.sess = sess
			}
			label := "default"
			if m.thinkBudget > 0 {
				label = strconv.Itoa(m.thinkBudget) + " chars"
			}
			m.appendBlock(markerBlock("Think budget: " + label))
		})
		return m, nil
	case "budget-context":
		n, err := strconv.Atoi(strings.TrimSpace(arg))
		if err != nil || n < 0 {
			m.appendBlock(errorBlock("Usage: /budget-context N (0 = default)"))
			return m, nil
		}
		m.confirmRestart(func() {
			m.contextLimit = n
			if m.sess != nil {
				sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
				if err != nil {
					m.appendBlock(errorBlock(err.Error()))
					return
				}
				m.sess = sess
			}
			label := "default"
			if m.contextLimit > 0 {
				label = strconv.Itoa(m.contextLimit) + " tokens"
			}
			m.appendBlock(markerBlock("Context budget: " + label))
		})
		return m, nil
	case "sidebar":
		m.sidebarHidden = !m.sidebarHidden
		m.fitBottom()
		m.refreshContent()
		state := "shown"
		if m.sidebarHidden {
			state = "hidden"
		}
		m.appendBlock(markerBlock("Sidebar: " + state))
		return m, nil
	case "details":
		m.transcript.compact = !m.transcript.compact
		m.refreshContent()
		state := "full"
		if m.transcript.compact {
			state = "truncated"
		}
		m.appendBlock(markerBlock("AI replies: " + state))
		return m, nil
	case "sys":
		m.hideSYS = !m.hideSYS
		m.refreshContent()
		state := "shown"
		if m.hideSYS {
			state = "hidden"
		}
		m.appendBlock(markerBlock("SYS messages: " + state))
		return m, nil
	case "labels":
		m.hideLabels = !m.hideLabels
		m.refreshContent()
		state := "shown"
		if m.hideLabels {
			state = "hidden"
		}
		m.appendBlock(markerBlock("Labels: " + state))
		return m, nil
	case "latest":
		m.transcript.Viewport.GotoBottom()
		m.transcript.Follow = true
		return m, nil
	case "stop":
		if m.state != stRunning {
			m.appendBlock(markerBlock("nothing running"))
			return m, nil
		}
		m.interrupted = true
		m.cancel()
		m.state = stStopping
		m.appendBlock(markerBlock("Stopping run..."))
		m.fitBottom()
		return m, nil
	case "rename":
		m.renameActive()
		return m, nil
	case "delete":
		m.deleteActive()
		return m, nil
	case "permissions":
		lines := []string{"Plan exposes read-only tools. Act uses the configured policy and approval prompts.", "Settings changes do not approve tool execution.", "", "Policy rules (later rules take precedence)"}
		lines = append(lines, m.policyDetails...)
		m.openDialog("Permissions", lines)
		return m, nil
	case "model":
		transport := "Request / response"
		if m.streaming {
			transport = "Streaming"
		}
		m.openDialog("Model", []string{
			"Model: " + nonEmpty(m.modelName),
			"Backend: " + nonEmpty(m.backendKind),
			"Endpoint: " + nonEmpty(m.backendURL),
			"Transport: " + transport,
			fmt.Sprintf("Context window: %d tokens", m.limit),
			fmt.Sprintf("Response budget: %d tokens", m.responseCap),
		})
		return m, nil
	case "context":
		rows := []string{"Context: " + strings.Replace(m.meter(), "ctx ", "", 1), "Source: " + nonEmpty(m.usage.source)}
		if !m.usage.at.IsZero() {
			rows = append(rows, "Measured: "+m.usage.at.Format("15:04:05"))
		}
		if m.usage.last.Tokens > 0 {
			kind := "reported"
			if m.usage.last.Estimated {
				kind = "estimated"
			}
			rows = append(rows, fmt.Sprintf("Last request: %s tokens (%s)", kTokens(m.usage.last.Tokens), kind))
			rows = append(rows, "Request time: "+m.usage.last.At.Format("Jan 2 15:04:05"))
		}
		if m.usage.source == "Saved history estimate" {
			rows = append(rows, "Estimate excludes tool schemas and images.")
		}
		rows = append(rows, "/compact is available for coding task history.")
		m.openDialog("Context usage", rows)
		return m, nil
	case "run":
		rows := []string{fmt.Sprintf("Steps: %d / %d", m.steps, m.maxSteps), "Elapsed: " + formatElapsed(m.elapsed)}
		if !m.firstToken.IsZero() {
			rows = append(rows, "First token: "+formatElapsed(m.firstToken.Sub(m.started)))
		}
		rows = append(rows, fmt.Sprintf("Tools: %d   Files: %d", m.toolsUsed, len(m.filesTouched)))
		m.openDialog("Run", rows)
		return m, nil
	case "activity":
		rows := []string{"State: " + m.statusWord(), "Elapsed: " + formatElapsed(m.elapsed), fmt.Sprintf("Steps: %d / %d", m.steps, m.maxSteps)}
		if m.runErr != nil {
			rows = append(rows, "Error: "+m.runErr.Error())
			if m.retryRun != nil {
				rows = append(rows, "Use /retry to retry the failed turn.")
			}
		}
		if m.busy() {
			rows = append(rows, "Use /stop to stop the run.")
		}
		m.openDialog("Activity", rows)
		return m, nil
	case "about":
		m.openDialog("About", []string{"Local coding assistant", "Go · Bubble Tea"})
		return m, nil
	case "theme":
		arg = strings.ToLower(strings.TrimSpace(arg))
		if arg == "" {
			m.appendBlock(markerBlock("Theme: dark or light · /theme dark|light"))
			return m, nil
		}
		if arg != "dark" && arg != "light" {
			m.appendBlock(errorBlock("Choose /theme dark or /theme light."))
			return m, nil
		}
		setTheme(arg)
		m.appendBlock(markerBlock("Theme: " + arg))
		return m, nil
	case "settings":
		if m.config == nil {
			m.appendBlock(errorBlock("no settings available"))
			return m, nil
		}
		m.editSettings("application")
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
		m.confirmRestart(func() {
			m.nudges = parsed
			if m.sess != nil {
				sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
				if err != nil {
					m.appendBlock(errorBlock(err.Error()))
					return
				}
				m.sess = sess
			}
			m.appendBlock(markerBlock("Nudges: " + agent.NudgeLevelName(m.nudges)))
		})
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
		m.confirmRestart(func() {
			previous := m.plan
			m.plan = mode == "plan"
			if m.sess != nil {
				sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
				if err != nil {
					m.plan = previous
					m.appendBlock(errorBlock(err.Error()))
					return
				}
				if err := m.saveMode(); err != nil {
					m.plan = previous
					m.appendBlock(errorBlock(err.Error()))
					return
				}
				m.sess = sess
			}
			m.appendBlock(markerBlock("Mode: " + m.modeName()))
		})
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