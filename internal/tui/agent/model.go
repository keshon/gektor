package agent

import (
	"context"
	"strconv"
	"sync"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/mcpmanager"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/retrospective"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/tui/components/completion"
	"github.com/keshon/gektor/internal/tui/components/composer"
	"github.com/keshon/gektor/internal/tui/components/gate"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/workspace"
)

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

type eventMsg harness.Event

type doneMsg struct {
	answer string
	err    error
}

type tickMsg time.Time

type liveFlushMsg struct{}

// Model owns the conversation, composer, inspector, and Agent execution.
// Shared menu and overlay models are supplied by the application shell.
type Model struct {
	hidden           bool
	shutdown         context.CancelFunc
	requests         []tea.Msg
	chrome           *screen.Controls
	steering         bool
	inspector        toolInspector
	liveFlushPending bool
	progress         llm.Progress
	identityUser     string
	identityName     string
	application      *settings.Application
	// config is the service set this launch was composed with. It is the
	// only route to the profile, so a nil value means "not configurable"
	// rather than "defaulted".
	config      *app.PreparedProfile
	modeChange  *ModeChange
	seedHistory []llm.Message

	transcript                transcriptState
	input                     composer.Model
	suggestions               completion.State
	gatePopup                 gate.State
	queued                    string
	queuedVersion             uint64
	suspendedInputEdit        *inputQueueEdit
	draftImages, queuedImages []string
	clipboardBusy             bool
	attachmentEpoch           uint64
	progressStarted           time.Time
	evaluation                *evaluationJob
	gateDraft                 string
	events                    chan harness.Event
	hub                       *harness.GateHub
	cancel                    context.CancelFunc
	// base spawns one context per run: an interrupted run's cancelled
	// context must never leak into the next turn.
	base  context.Context
	sess  *harness.Session
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
	mcp          *mcpmanager.Manager
	mcpBusy      bool
	mcpTable     *screen.TableState
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
	titleJob     *sessionTitleJob
	// ws resolves @file paths against the workspace, escape-checked
	// like every file tool path. Stored, not rebuilt per turn.
	ws *workspace.Workspace
	// newSession builds a session for a fresh task (first run and
	// /new). Follow-ups reuse the running session via Resume.
	newSession func(task, stateFile string, images []string) (*harness.Session, error)
	// retryRun restarts the last turn on the update loop, taking fresh service
	// snapshots before starting workers. Set by startRun, gated by runErr.
	retryRun func()
	// send queues run completion after prior harness events. New binds it
	// before any worker starts; workers never access the UI model.
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
	gatePreview  string
	note         textinput.Model
	alwaysMu     sync.Mutex
	always       map[[2]string]bool
	lastLesson   retrospective.Lesson
	lessonRoot   string
	// scrollDrag tracks a scrollbar thumb grab: active while the pointer
	// is held on the bar, grab is the pointer's row inside the thumb so
	// the thumb follows without jumping, moved marks real motion so a
	// press-and-release without motion still counts as a click.
	scrollDrag      scrollDrag
	sessions        *sessionsState
	sessionsRoot    string
	savedSessions   []sessionEntry
	sessionsLoading bool
	// View-menu display filters. They hide transcript rows, never model
	// history: a hidden block is still sent, stored and re-rendered the
	// moment the filter is turned off. All choices are global preferences.
	hideSYS    bool
	hideLabels bool
	hideNames  bool
	minimal    bool
	hideTools  bool

	sessionIndex    map[string]cachedSession
	sessionRevision uint64
	pendingCommands []tea.Cmd
	drafts          Drafts
	plan            bool

	gateVP viewport.Model
	// Status facts snapshotted from cfg at construction for /status:
	// backend/workspace identity and budgets the run was given.
	backendKind   string
	accountLabel  string
	backendURL    string
	responseCap   int
	streaming     bool
	modelName     string
	wsRoot        string
	mcpCount      int
	policyRules   int
	policyDetails []permission.Rule
}

func gateEvent(name string, fields map[string]any) harness.Event {
	prompt, _ := fields["prompt"].(string)
	tool, _ := fields["tool"].(string)
	resource, _ := fields["resource"].(string)
	return harness.Event{Name: name, Fields: map[string]any{
		"kind": fields["kind"], "prompt": prompt, "tool": tool, "resource": resource,
	}}
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

func (m *Model) sessionEnv() roles.Env {
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
func (m *Model) stepBudget() string {
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
func (m *Model) stepLine() string {
	return "Steps: " + strconv.Itoa(m.steps) + "/" + m.stepBudget()
}

// Close cancels screen operations, including title generation and evaluation.
func (m *Model) Close() {
	if m.shutdown != nil {
		m.shutdown()
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.cancelSessionTitle()
	m.cancelEvaluation()
}

// Finish persists local preferences and returns the completed run or relaunch request.
func (m *Model) Finish(quit bool) (string, error) {
	if m.modeChange != nil {
		return "", m.modeChange
	}
	if err := m.savePreferences(); err != nil {
		return "", err
	}
	if quit || m.quit {
		return "", nil
	}
	return m.answer, m.runErr
}
