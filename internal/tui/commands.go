package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/tui/components/completion"
)

// command is one slash command.
//
// arg is what makes Enter mean something. Handing "/help" to the composer
// and waiting for a second Enter is right for "/max-steps" and wrong for
// "/help", and a flat list of names cannot tell them apart — so the popup
// had to treat every command as if it took an argument.
//
// immediate is the other thing a name cannot say about itself: whether it
// touches the run in progress. Queuing is right for anything the model is
// in the middle of, and wrong for the view toggles, the fact dialogs and
// /stop — a command you reach for mid-run, which then waits for the run it
// was meant to act on. The two are not the same list, and a command that
// takes an argument used to fall through to the queue by accident, because
// its second Enter no longer had a picker to run it.
type command struct {
	name      string
	arg       string   // placeholder after the name; empty means it runs bare
	choices   []string // fixed values, offered once the name is typed
	immediate bool     // runs during a run instead of queueing behind it
	summary   string
}

func (c command) reference() [2]string {
	if c.arg == "" {
		return [2]string{c.name, c.summary}
	}
	return [2]string{c.name + " " + c.arg, c.summary}
}

func (c command) takesArgument() bool { return c.arg != "" }

// commands is the single list of slash commands. Help, completion and
// execution all read it, so a command cannot exist in one and not another.
func commands() []command {
	return []command{
		{name: "/help", immediate: true, summary: "Open this help"},
		{name: "/sessions", immediate: true, summary: "Browse saved sessions"},
		{name: "/new", arg: "[task]", summary: "Start a fresh task; omit task for an empty session"},
		{name: "/mode", arg: "plan|act", choices: []string{"plan", "act"}, summary: "Preview changes or execute them"},
		{name: "/max-steps", arg: "N", summary: "Step budget (0 = no limit)"},
		{name: "/budget-think", arg: "N", summary: "Reasoning budget in chars (0 = default)"},
		{name: "/nudges", arg: "full|recovery|off", choices: []string{"full", "recovery", "off"}, summary: "How much harness advice the model hears"},
		{name: "/sidebar", immediate: true, summary: "Toggle sidebar"},
		{name: "/details", immediate: true, summary: "Toggle reply truncation"},
		{name: "/sys", immediate: true, summary: "Toggle SYS messages"},
		{name: "/labels", immediate: true, summary: "Toggle labels"},
		{name: "/latest", immediate: true, summary: "Jump to latest"},
		{name: "/stop", immediate: true, summary: "Stop the running task"},
		{name: "/rename", arg: "[title]", summary: "Rename the current session"},
		{name: "/delete", summary: "Delete the current session"},
		{name: "/permissions", immediate: true, summary: "Show permission policy"},
		{name: "/model", immediate: true, summary: "Show model and backend"},
		{name: "/context", immediate: true, summary: "Show context usage"},
		{name: "/run", immediate: true, summary: "Show run stats"},
		{name: "/activity", immediate: true, summary: "Show run activity"},
		{name: "/about", immediate: true, summary: "About"},
		{name: "/theme", arg: "dark|light", choices: []string{"dark", "light"}, immediate: true, summary: "Switch theme"},
		{name: "/settings", immediate: true, summary: "Open settings"},
		{name: "/status", immediate: true, summary: "Show current run facts"},
		{name: "/retry", summary: "Retry the last failed turn"},
		{name: "/compact", summary: "Shrink this session's history"},
		{name: "/quit", immediate: true, summary: "Quit"},
	}
}

// lookupCommand finds a command by the name typed, or nil.
func lookupCommand(name string) *command {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, c := range commands() {
		if c.name == name {
			found := c
			return &found
		}
	}
	return nil
}

// commandTakesArgument reports whether Enter should hand the command to the
// composer instead of running it. A command that takes nothing is ready the
// moment it is named, and asking for a second Enter on it is busywork.
func commandTakesArgument(value string) bool {
	name, _, _ := strings.Cut(value, " ")
	c := lookupCommand(name)
	return c != nil && c.takesArgument()
}

// runsImmediate reports whether a command acts on something other than the
// run in progress: the view, a fact dialog, the agent's own services. It
// reads the command's own declaration, so adding a command cannot leave it
// queueing by default unnoticed.
func runsImmediate(value string) bool {
	name, _, _ := strings.Cut(strings.TrimSpace(value), " ")
	c := lookupCommand(name)
	return c != nil && c.immediate
}

// runsOnEnter reports whether Enter runs this text where it stands.
//
// Two things say no. A command that still needs an argument is not ready. A
// ready command is not ready either if a run is in progress and it touches
// that run: the picker would otherwise run /compact against a state file the
// run is still writing, which is the mistake the queue exists to prevent.
func (m *model) runsOnEnter(value string) bool {
	if commandTakesArgument(value) {
		return false
	}
	return !m.busy() || runsImmediate(value)
}

func init() {
	// The picker renders its own hint and needs the same answer, but the
	// registry lives here. One lookup, injected, rather than a second list
	// that could disagree about which commands take an argument.
	completion.SetRunner(commandTakesArgument)
}

// Shared by help and command completion.
func commandReference() [][2]string {
	cmds := commands()
	out := make([][2]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.reference()
	}
	return out
}

// confirmRestart asks before restarting a running session. It uses the same
// destructive form as deleting a session: nothing to type, the choice lands
// on Cancel, and Enter only acts on whatever is selected.
//
// If no session is running the action runs immediately: there is nothing to
// lose, and a prompt the user cannot act on is worse than no prompt.
func (m *model) confirmRestart(action func()) {
	if m.sess == nil {
		action()
		return
	}
	m.openForm(
		"Restart agent",
		"TARS will restart to apply this. The conversation and draft are preserved.",
		"Restart", nil,
		func(map[string]string) error {
			action()
			return nil
		},
	)
	m.dialog.form.Destructive = true
	// Never open a destructive choice on the destructive one.
	m.focusForm(1)
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
			previous := m.maxStepsOverride
			if n == 0 {
				n = noStepLimit
			}
			m.maxStepsOverride = n
			if m.sess != nil {
				sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
				if err != nil {
					m.maxStepsOverride = previous
					m.appendBlock(errorBlock(err.Error()))
					return
				}
				m.sess = sess
			}
			m.appendBlock(markerBlock("Max steps: " + m.stepBudget()))
		})
		return m, nil
	case "budget-think":
		n, err := strconv.Atoi(strings.TrimSpace(arg))
		if err != nil || n < 0 {
			m.appendBlock(errorBlock("Usage: /budget-think N (0 = default)"))
			return m, nil
		}
		m.confirmRestart(func() {
			previous := m.thinkBudget
			m.thinkBudget = n
			if m.sess != nil {
				sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
				if err != nil {
					m.thinkBudget = previous
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
		rows := []string{m.stepLine(), "Elapsed: " + formatElapsed(m.elapsed)}
		if !m.firstToken.IsZero() {
			rows = append(rows, "First token: "+formatElapsed(m.firstToken.Sub(m.started)))
		}
		rows = append(rows, fmt.Sprintf("Tools: %d   Files: %d", m.toolsUsed, len(m.filesTouched)))
		m.openDialog("Run", rows)
		return m, nil
	case "activity":
		rows := []string{"State: " + m.statusWord(), "Elapsed: " + formatElapsed(m.elapsed), m.stepLine()}
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
		if err := m.persistTheme(arg); err != nil {
			m.appendBlock(errorBlock(err.Error()))
			return m, nil
		}
		m.applyTheme(arg)
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
			previous := m.nudges
			m.nudges = parsed
			if m.sess != nil {
				sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
				if err != nil {
					m.nudges = previous
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
