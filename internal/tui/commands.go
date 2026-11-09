package tui

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/config"
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
		{name: "/workflows", immediate: true, summary: "Open workflow canvas"},
		{name: "/mcp", immediate: true, summary: "Manage MCP servers"},
		{name: "/processes", immediate: true, summary: "Inspect and stop managed background processes"},
		{name: "/new", arg: "[task]", summary: "Start a fresh task; omit task for an empty session"},
		{name: "/mode", arg: "plan|act", choices: []string{"plan", "act"}, summary: "Preview changes or execute them"},
		{name: "/max-steps", arg: "N", summary: "Step budget (0 = no limit)"},
		{name: "/steering", arg: "on|off", choices: []string{"on", "off"}, immediate: true, summary: "Let new input withhold unstarted tools"},
		{name: "/details", immediate: true, summary: "Toggle full agent thinking"},
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
		{name: "/eval", immediate: true, summary: "Review last run; /eval session or /eval N [focus]"},
		{name: "/learn", immediate: true, summary: "Learn from last run; N/session select, review reopens, undo disables"},
		{name: "/theme", arg: "name", choices: config.ThemeNames(), immediate: true, summary: "Switch theme"},
		{name: "/settings", immediate: true, summary: "Open settings"},
		{name: "/status", immediate: true, summary: "Show current run facts"},
		{name: "/retry", summary: "Retry the last failed turn"},
		{name: "/compact", summary: "Shrink this session's history"},
		{name: "/quit", immediate: true, summary: "Quit"},
		{name: "/paste-image", immediate: true, summary: "Attach an image from the clipboard"},
		{name: "/detach", arg: "N", immediate: true, summary: "Remove a draft image by number"},
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
		"GEKTOR will restart to apply this. The conversation and draft are preserved.",
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
	name = strings.ToLower(name)
	m.input.Push(text)
	if m.evaluation != nil && name != "stop" && name != "quit" && name != "q" {
		return m, nil
	}
	switch strings.ToLower(name) {
	case "workflows":
		return m, m.workflowCommand(strings.TrimSpace(arg))
	case "mcp":
		action, id, _ := strings.Cut(strings.TrimSpace(arg), " ")
		if action == "" {
			m.openMCP()
			return m, nil
		}
		return m, m.handleMCPAction(mcpActionMsg{action, id})
	case "paste-image":
		return m, m.pasteClipboard(true)
	case "detach":
		m.detachImage(arg)
		return m, nil
	case "eval":
		return m.evaluate(arg)
	case "learn":
		return m.learn(arg)
	case "q", "quit":
		m.cancelEvaluation()
		return m.requestQuit()
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
		m.newTask()
		if err := m.startFreshPrompt(clean, task, images); err != nil {
			m.appendBlock(errorBlock("cannot start: " + err.Error()))
			return m, nil
		}
		return m, nil
	case "status":
		m.appendBlock(markerBlock(strings.Join(m.statusLines(), "\n")))
		return m, nil
	case "max-steps":
		return m.commandStepLimit(arg)
	case "steering":
		value := strings.ToLower(strings.TrimSpace(arg))
		if value != "on" && value != "off" {
			m.appendBlock(errorBlock("Usage: /steering on|off"))
			return m, nil
		}
		previous := m.steering
		m.steering = value == "on"
		if err := m.saveHarness(); err != nil {
			m.steering = previous
			m.appendBlock(errorBlock(err.Error()))
			return m, nil
		}
		if m.sess != nil {
			m.sess.SetSteering(m.steering)
		}
		return m, nil
	case "details":
		return m.toggleThinking()
	case "sys":
		if err := m.changeAppearance(func(a *config.Appearance) { a.HideSystem = !a.HideSystem }); err != nil {
			m.openDialog("Appearance", []string{err.Error()})
		}
		return m, nil
	case "labels":
		if err := m.changeAppearance(func(a *config.Appearance) { a.HideLabels = !a.HideLabels }); err != nil {
			m.openDialog("Appearance", []string{err.Error()})
			return m, nil
		}
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
		if m.evaluation != nil {
			m.closeDialog()
			return m, nil
		}
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
	case "processes":
		m.openProcesses(nil)
		return m, nil
	case "rename":
		m.renameActive()
		return m, nil
	case "delete":
		m.deleteActive()
		return m, nil
	case "permissions":
		m.openPermissions()
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
		return m.commandContext()
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
				rows = append(rows, recoveryHint(m.runErr))
			}
		}
		if m.busy() {
			rows = append(rows, "Use /stop to stop the run.")
		}
		m.openDialog("Activity", rows)
		return m, nil
	case "about":
		m.openDialog("GEKTOR", []string{
			"Terminal coding agent written in Go.",
			"",
			m.styles.gate.Render("Capabilities"),
			"Read, search and edit files, run commands and tests, and resume saved sessions.",
			"Plan before changes or work directly in Act mode; tool permissions stay visible.",
			"",
			m.styles.gate.Render("Model backends"),
			"llama.cpp · KoboldCPP · OpenAI-compatible APIs",
			"Models and inference servers are supplied separately.",
			"",
			m.styles.gate.Render("Repository"),
			"https://github.com/keshon/gektor",
			"Go " + strings.TrimPrefix(runtime.Version(), "go") + " · Bubble Tea · " + runtime.GOOS + "/" + runtime.GOARCH,
		})
		return m, nil
	case "theme":
		arg = strings.ToLower(strings.TrimSpace(arg))
		if arg == "" {
			m.appendBlock(markerBlock("Themes: " + strings.Join(config.ThemeNames(), ", ") + ". Use /theme <name>."))
			return m, nil
		}
		if !config.ValidTheme(arg) {
			m.appendBlock(errorBlock("Unknown theme. Use /theme to list available themes."))
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
		m.editSettings("workspace")
		return m, nil
	case "mode":
		return m.commandMode(arg)
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

func (m *model) commandStepLimit(arg string) (tea.Model, tea.Cmd) {
	n, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || n < 0 {
		m.appendBlock(errorBlock("Usage: /max-steps N (0 = no limit)"))
		return m, nil
	}
	m.confirmRestart(func() {
		previous := m.maxStepsOverride
		previousSession := m.sess
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
		if err := m.saveHarness(); err != nil {
			m.maxStepsOverride = previous
			m.sess = previousSession
			m.appendBlock(errorBlock(err.Error()))
			return
		}
		m.appendBlock(markerBlock("Max steps: " + m.stepBudget()))
	})
	return m, nil
}

func (m *model) commandMode(arg string) (tea.Model, tea.Cmd) {
	mode := strings.ToLower(strings.TrimSpace(arg))
	if mode == "" {
		m.appendBlock(markerBlock("Mode: " + m.modeName() + " · /mode plan previews changes · /mode act executes them"))
		return m, nil
	}
	if mode != "plan" && mode != "act" {
		m.appendBlock(errorBlock("Choose /mode plan or /mode act."))
		return m, nil
	}
	if (mode == "plan") == m.plan {
		if err := m.saveDefaultMode(); err != nil {
			m.appendBlock(errorBlock(err.Error()))
			return m, nil
		}
		m.appendBlock(markerBlock("Mode: " + m.modeName()))
		return m, nil
	}
	// The system prompt and tool set do not change with the mode, only
	// what the loop refuses and which stance turn it appends, so the
	// rebuild below cannot cost the backend its cached prefix. An idle
	// switch is free and needs no confirmation; one that would replace a
	// session mid-run keeps the existing destructive prompt.
	apply := func() error {
		previous := m.plan
		nextSession := m.sess
		m.plan = mode == "plan"
		if m.sess != nil {
			sess, err := m.newSession(m.activeSessionTitle(), m.stateFile, nil)
			if err != nil {
				m.plan = previous
				return err
			}
			if err := m.saveMode(); err != nil {
				m.plan = previous
				return err
			}
			nextSession = sess
		} else if err := m.saveMode(); err != nil {
			m.plan = previous
			return err
		}
		if err := m.saveDefaultMode(); err != nil {
			m.plan = previous
			if m.stateFile != "" {
				_ = writeSavedMode(m.stateFile, previous)
			}
			return err
		}
		m.sess = nextSession
		m.appendBlock(markerBlock("Mode: " + m.modeName()))
		return nil
	}
	if m.state == stRunning {
		m.confirmRestart(func() {
			if err := apply(); err != nil {
				m.appendBlock(errorBlock(err.Error()))
			}
		})
		return m, nil
	}
	if err := apply(); err != nil {
		m.appendBlock(errorBlock(err.Error()))
	}
	return m, nil
}

func (m *model) commandContext() (tea.Model, tea.Cmd) {
	rows := []string{"Context: " + strings.Replace(m.meter(), "ctx ", "", 1), "Source: " + nonEmpty(m.usage.source)}
	if report := m.usage.breakdown; report.EstimatedTokens > 0 {
		rows = append(rows, fmt.Sprintf("Request estimate: ~%d tokens", report.EstimatedTokens))
		for _, section := range report.Sections {
			if section.EstimatedTokens > 0 {
				rows = append(rows, fmt.Sprintf("  %s: ~%d", section.Name, section.EstimatedTokens))
			}
		}
		rows = append(rows, "Largest tool schemas (estimates):")
		for _, tool := range report.Tools[:min(5, len(report.Tools))] {
			rows = append(rows, fmt.Sprintf("  %s: ~%d", tool.Name, tool.EstimatedTokens))
		}
		if report.BackendTokens > 0 {
			kind := "measured"
			if report.BackendEstimated {
				kind = "estimated"
			}
			rows = append(rows, fmt.Sprintf("Backend prompt: %d (%s); cached: %d (still occupies context)", report.BackendTokens, kind, report.CachedTokens))
		}
	}
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
}
