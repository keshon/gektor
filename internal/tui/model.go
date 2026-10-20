package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/audit"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/run"
	"github.com/keshon/gektor/internal/tui/components/completion"
	"github.com/keshon/gektor/internal/tui/components/composer"
	"github.com/keshon/gektor/internal/tui/components/gate"
	"github.com/keshon/gektor/internal/workspace"
)

// Config wires one TUI run. Env carries the full harness (client,
// workspace, tools, policy, budgets); gate behavior comes from the TUI,
// so Env.Gate is overridden, never read.
type Config struct {
	SessionDirectory string
	Application      *Application
	Services         *app.Services
	SeedHistory      []llm.Message
	InitialDraft     string
	BackendURL       string
	Plan             bool
	History          []llm.Message
	ResumeAnswer     string

	Env       roles.Env
	Task      string
	StateFile string
	Verify    func(ctx context.Context) (output string, err error)
	// Images attaches pictures to the opening task (-image with
	// -tui); follow-up turns attach their own @paths per turn.
	Images []string
	// AuditPath, when set, appends gate decisions as JSONL (see audit).
	AuditPath string
}
type runState int

const (
	stRunning runState = iota
	stPermission
	stAsk
	stDone
	stStopping
)

// Step-budget sentinels. The loop reads a non-positive MaxSteps as "use the
// default", so an unlimited run is a large number rather than zero.
const (
	noStepLimit    = -1
	unlimitedSteps = config.UnlimitedSteps
)

type eventMsg run.Event
type doneMsg struct {
	answer string
	err    error
}
type tickMsg time.Time
type liveFlushMsg struct{}
type model struct {
	liveFlushPending bool
	progress         llm.Progress
	identityUser     string
	identityName     string
	application      *Application
	// config is the service set this launch was composed with. It is the
	// only route to the profile, so a nil value means "not configurable"
	// rather than "defaulted".
	config      *app.Services
	modeChange  *ModeChange
	seedHistory []llm.Message

	transcript  transcriptState
	input       composer.Model
	suggestions completion.State
	gatePopup   gate.State
	queued      string
	evaluation  *evaluationJob
	gateDraft   string
	events      chan run.Event
	hub         *run.GateHub
	cancel      context.CancelFunc
	// base spawns one context per run: an interrupted run's cancelled
	// context must never leak into the next turn.
	base  context.Context
	sess  *run.Session
	state runState
	gate  string
	steps int
	// maxSteps is the run's step budget for the meter: the loop default,
	// since the Interactive role never overrides it. Display only — the
	// loop enforces its own copy.
	maxSteps int
	// maxStepsOverride is the user's /max-steps setting: noStepLimit for
	// none, 0 for the configured default, N > 0 for N steps.
	maxStepsOverride int
	// baseEnv is the launch environment before the slash-command
	// overrides; sessionEnv layers those on per session.
	baseEnv      roles.Env
	started      time.Time
	elapsed      time.Duration
	spinnerFrame int
	ready        bool
	answer       string
	runErr       error
	styles       styles
	quit         bool
	// interrupted marks a run stopped by esc rather than completion;
	// its doneMsg renders neutrally instead of as an error.
	interrupted bool
	// stateFile locates the saved transcript for follow-ups.
	stateFile    string
	sessionTitle string
	// ws resolves @file paths against the workspace, escape-checked
	// like every file tool path. Stored, not rebuilt per turn.
	ws *workspace.Workspace
	// newSession builds a session for a fresh task (first run and
	// /new). Follow-ups reuse the running session via Resume.
	newSession func(task, stateFile string, images []string) (*run.Session, error)
	// retryRun restarts the last turn on the update loop, taking fresh service
	// snapshots before starting workers. Set by startRun, gated by runErr.
	retryRun func()
	// send delivers out-of-band messages (run completion) to the
	// program from worker goroutines. Set by Run once tea exists.
	send func(tea.Msg)
	// termW/termH are the last reported terminal dimensions, kept so
	// state transitions can resize the viewport to fit the bottom bar.
	termW int
	termH int
	// tokens is the current prompt measurement or saved-history estimate; limit is the
	// context window from Env. Together they drive the status meter.
	// limit is display only and never overridden: the window is the server's
	// to report, so the harness does not get to pretend otherwise.
	tokens int
	limit  int
	// tokensEst marks estimated numbers (streaming backends that report
	// no usage block); the meter prefixes "~" so estimates never pose
	// as measurements.
	tokensEst bool
	usage     usageState
	header    headerState

	// live is the streamed reply already revealed to the transcript;
	// liveQueue holds deltas received but not yet revealed, so a reply
	// appears a few characters at a time instead of landing in whatever
	// chunks the backend happened to send. liveRunes counts live plus
	// liveQueue so the cap is checked in constant time per delta.
	live      string
	liveQueue string
	liveCut   bool
	liveRunes int
	// liveThink and liveThinkQueue mirror the pair above for streamed
	// deliberation. Keeping them apart is what makes the turn read as
	// THINK then RESPONSE: the reasoning block fills first and stays
	// visible while the answer streams underneath it. One merged buffer
	// would interleave them, and reasoning arriving at the end would blank
	// the answer to make room.
	liveThink      string
	liveThinkQueue string
	// lastSpin gates the braille spinner. The tick drops to a frame
	// while a reply reveals, and the spinner must not spin up with it.
	lastSpin time.Time
	// firstToken stamps the first streamed chunk of the run: time to
	// first word beside elapsed time. Zero until streaming starts;
	// cleared with live at every turn boundary.
	firstToken time.Time
	// toolsUsed counts tool calls this run; filesTouched collects
	// observed changed paths. They reset with the step counter in startRun.
	toolsUsed    int
	filesTouched map[string]bool
	overlays     []overlayFrame
	gstage       gateStage
	gateTool     string
	gateResource string
	note         textinput.Model
	alwaysMu     sync.Mutex
	always       map[[2]string]bool
	dialog       *infoDialog
	// scrollDrag tracks a scrollbar thumb grab: active while the pointer
	// is held on the bar, grab is the pointer's row inside the thumb so
	// the thumb follows without jumping, moved marks real motion so a
	// press-and-release without motion still counts as a click.
	scrollDrag    scrollDrag
	sessions      *sessionsState
	nav           sessionsState
	navFocused    bool
	sidebarHidden bool
	// View-menu display filters. They hide transcript rows, never model
	// history: a hidden block is still sent, stored and re-rendered the
	// moment the filter is turned off. Session-only, so a restart always
	// shows the full transcript.
	hideSYS    bool
	hideLabels bool

	sessionIndex    map[string]cachedSession
	sessionRevision uint64
	// savedSettings remembers the bytes last written, so an unchanged
	// preferences write is skipped rather than fsynced again.
	savedSettings   string
	pendingCommands []tea.Cmd
	drafts          map[string]draft
	plan            bool

	gateVP viewport.Model
	// Status facts snapshotted from cfg at construction for /status:
	// backend/workspace identity and budgets the run was given.
	backendKind   string
	backendURL    string
	responseCap   int
	streaming     bool
	modelName     string
	wsRoot        string
	mcpCount      int
	policyRules   int
	policyDetails []string
}

func gateEvent(name string, fields map[string]any) run.Event {
	prompt, _ := fields["prompt"].(string)
	tool, _ := fields["tool"].(string)
	resource, _ := fields["resource"].(string)
	return run.Event{Name: name, Fields: map[string]any{
		"kind": fields["kind"], "prompt": prompt, "tool": tool, "resource": resource,
	}}
}

// withoutPrintHooks strips the CLI print hooks from Env for fullscreen
// runs: stepPrinter-style callbacks (steps, findings, nudges) write to
// stdout, racing the alt-screen renderer (stale status rows plus leaked
// "[step N]" lines that heal only on resize). The TUI reads the event
// stream, never the hooks; no-ops keep every call site safe without
// auditing each one.
func withoutPrintHooks(env roles.Env) roles.Env {
	env.OnDelta = func(string) {}
	env.OnStep = func(string, int, llm.Message) {}
	env.OnResult = func(agent.ToolResult) {}
	env.OnToolOutput = func(string, string) {}
	env.OnUsage = func(int, llm.Usage) {}
	env.OnNudge = nil
	env.OnInput = nil
	return env
}

// envMaxSteps mirrors the loop default: Env.MaxSteps set by -max-steps,
// otherwise the agent default. Display only — like the other status
// facts it is snapshotted once; the live meter follows max_steps events.
func envMaxSteps(env roles.Env) int {
	if env.MaxSteps > 0 {
		return env.MaxSteps
	}
	return agent.DefaultMaxSteps
}

func (m *model) sessionEnv() roles.Env {
	env := m.baseEnv
	switch {
	case m.maxStepsOverride == noStepLimit:
		env.MaxSteps = unlimitedSteps
	case m.maxStepsOverride > 0:
		env.MaxSteps = m.maxStepsOverride
	}
	return env
}

// stepBudget renders the enforced step ceiling for display. "no limit" is a
// setting rather than a number: printing the sentinel would read as a real
// budget, and printing zero would read as a finished one.
func (m *model) stepBudget() string {
	if m.maxStepsOverride == noStepLimit {
		return "∞"
	}
	if m.maxStepsOverride > 0 {
		return strconv.Itoa(m.maxStepsOverride)
	}
	if m.maxSteps == unlimitedSteps {
		return "∞"
	}
	return strconv.Itoa(m.maxSteps)
}

// stepLine is the one phrasing of "steps so far / steps allowed", shared by
// every place that reports run facts.
func (m *model) stepLine() string {
	return "Steps: " + strconv.Itoa(m.steps) + "/" + m.stepBudget()
}

// Run executes the task under the TUI and returns the final answer.
// The terminal is restored on return, however the run ends.
func Run(ctx context.Context, cfg Config) (string, error) {
	uiCtx, cancelUI := context.WithCancel(ctx)
	defer cancelUI()
	eventsCh := make(chan run.Event, 256)
	hub := run.NewGateHub(func(name string, fields map[string]any) {
		select {
		case eventsCh <- gateEvent(name, fields):
		case <-uiCtx.Done():
		}
	})
	// startRun replaces this with the cancel for the run it derives from
	// base. It exists so m.cancel is never nil: the gate and quit paths
	// call it, and a TUI sitting idle has not started a run yet.
	_, cancel := context.WithCancel(uiCtx)
	if cfg.Application != nil {
		setTheme(cfg.Application.Preferences.Theme)
	}
	m := &model{
		identityName: cfg.Env.IdentityName,
		identityUser: cfg.Env.UserName,
		events:       eventsCh,
		hub:          hub,
		cancel:       cancel,
		base:         uiCtx,
		started:      time.Now(),
		styles:       defaultStyles(),
		limit:        cfg.Env.ContextLimit,
		maxSteps:     envMaxSteps(cfg.Env),
		transcript:   newTranscript(),
		plan:         cfg.Plan,

		stateFile: cfg.StateFile,
		note:      newNote(),
		always:    map[[2]string]bool{},
		// Status facts come from cfg once: the TUI never re-reads Env.
		backendKind: cfg.Env.BackendKind,
		backendURL:  safeEndpoint(cfg.BackendURL),
		responseCap: cfg.Env.MaxTokens,
		streaming:   cfg.Env.Stream && cfg.Env.BackendKind != "kobold",
		modelName:   cfg.Env.Model,
		ws:          cfg.Env.WS,
		wsRoot:      wsRootOf(cfg.Env.WS),
		mcpCount:    len(cfg.Env.MCPTools),
		policyRules: len(cfg.Env.Policy.Rules),
	}
	defer m.cancel()
	if cfg.Env.MaxSteps == unlimitedSteps {
		m.maxStepsOverride = noStepLimit
	}
	m.application = cfg.Application
	m.config = cfg.Services
	m.refreshNavigator()
	m.input = newInput()
	env := withoutPrintHooks(cfg.Env)
	env.Gate = nil
	env.GateContext = audit.ContextHook(cfg.AuditPath, "tui", m.contextGate(hub, cfg.Env.WS))
	m.baseEnv = env
	m.newSession = func(task, stateFile string, images []string) (*run.Session, error) {
		return run.New(run.Config{
			Env:          m.sessionEnv(),
			Task:         task,
			Plan:         m.plan,
			StateFile:    stateFile,
			Verify:       cfg.Verify,
			Images:       images,
			Answer:       hub.Suspender(),
			ResumeAnswer: cfg.ResumeAnswer,
			OnEvent: func(ev run.Event) {
				select {
				case eventsCh <- ev:
				case <-uiCtx.Done():
				}
			},
		})
	}
	for _, rule := range cfg.Env.Policy.Rules {
		m.policyDetails = append(m.policyDetails, fmt.Sprintf("%s · %s · %s", rule.Effect, rule.Tool, rule.Pattern))
	}
	m.seedHistory = cfg.SeedHistory
	if cfg.SessionDirectory != "" {
		if info, err := os.Stat(cfg.SessionDirectory); err != nil || !info.IsDir() {
			return "", fmt.Errorf("cannot open session directory %s", cfg.SessionDirectory)
		}
		m.stateFile = filepath.Join(cfg.SessionDirectory, "state.json")
		m.sessionTitle = workspace.ReadSessionTitle(cfg.SessionDirectory)
	}

	inputOptions, restoreInput, err := consoleInput(uiCtx)
	if err != nil {
		return "", err
	}
	defer restoreInput()
	prog := tea.NewProgram(m, append(inputOptions, tea.WithContext(uiCtx))...)
	m.send = func(msg tea.Msg) {
		if done, ok := msg.(doneMsg); ok {
			select {
			case eventsCh <- run.Event{Name: "_done", Fields: map[string]any{"answer": done.answer, "error": done.err}}:
			case <-uiCtx.Done():
			}
			return
		}
		prog.Send(msg)
	}
	switch {
	case len(cfg.History) > 0:
		task := cfg.Task
		if strings.TrimSpace(task) == "" {
			task = "Continue the saved task."
		}
		sess, err := m.newSession(task, m.stateFile, cfg.Images)
		if err != nil {
			return "", err
		}
		m.sess = sess
		m.transcript.blocks = append(m.transcript.blocks, renderHistory(cfg.History)...)
		m.restoreContext(cfg.History)
		m.startRun(func(runCtx context.Context) (string, error) {
			return sess.Resume(runCtx, cfg.History, task, cfg.Images...)
		})
	case strings.TrimSpace(cfg.Task) != "":
		if err := m.startInitial(cfg.Task, cfg.Images); err != nil {
			return "", err
		}
	default:
		m.state = stDone
		m.stateFile = ""
		m.appendBlock(markerBlock("New task · type a task to begin\n/mode plan previews changes · /mode act executes them\n@path attaches workspace files or images"))
		m.input.Focus()
	}
	if len(cfg.SeedHistory) > 0 || cfg.SessionDirectory != "" {
		if cfg.SessionDirectory != "" {
			m.stateFile = filepath.Join(cfg.SessionDirectory, "state.json")
		}
		m.transcript.blocks = renderHistory(cfg.SeedHistory)
		m.restoreContext(cfg.SeedHistory)
		m.refreshContent()
	}
	if cfg.SessionDirectory != "" {
		m.stateFile = filepath.Join(cfg.SessionDirectory, "state.json")
	}
	m.input.SetValue(cfg.InitialDraft)
	m.fitInput()
	m.fitBottom()
	if err := m.savePreferences(); err != nil {
		return "", err
	}
	final, err := prog.Run()
	if err != nil {
		cancel()
		return "", err
	}
	fm, ok := final.(*model)
	if !ok {
		return "", fmt.Errorf("tui: unexpected final model")
	}
	if fm.modeChange != nil {
		cancel()
		return "", fm.modeChange
	}
	if err := fm.saveApplication(); err != nil {
		return "", err
	}
	if fm.quit {
		cancel()
		return "", nil
	}
	return fm.answer, fm.runErr
}
