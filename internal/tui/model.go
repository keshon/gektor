// Package tui renders a run in the terminal: transcript, status, and
// gate prompts. It owns UI state and local session actions; agent execution
// and permissions use api.Session events and GateHub.
//
// Print mode remains the default; -tui renders direct, plan, and mission runs.
package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/api"
	"github.com/keshon/tars/internal/app"
	"github.com/keshon/tars/internal/audit"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/events"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/roles"
	"github.com/keshon/tars/internal/speech"
	"github.com/keshon/tars/internal/tui/components/composer"
	"github.com/keshon/tars/internal/workspace"
)

// Config wires one TUI run. Env carries the full harness (client,
// workspace, tools, policy, budgets); gate behavior comes from the TUI,
// so Env.Gate is overridden, never read.
type Config struct {
	// Runtime is borrowed when supplied; Chat and Audio must reference its
	// Services. Without it, Run owns a fallback runtime and closes it on exit.
	Runtime          *app.Runtime
	SessionDirectory string
	Application      *Application
	Audio            *ChatConfig
	SeedHistory      []llm.Message
	InitialDraft     string
	Chat             *ChatConfig
	BackendURL       string
	Plan             bool
	History          []llm.Message
	ResumeAnswer     string
	Mission          func(context.Context, roles.Env, agent.Suspender, *events.Emitter) error
	Env              roles.Env
	Task             string
	StateFile        string
	Verify           func(ctx context.Context) (output string, ok bool)
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
	stRecording
	stRecognizing
)

type eventMsg api.Event
type doneMsg struct {
	answer string
	err    error
}
type tickMsg time.Time
type model struct {
	identityUser             string
	identityName             string
	application              *Application
	audioConfig              *ChatConfig
	modeChange               *ModeChange
	seedHistory              []llm.Message
	listening                *speech.LiveCapture
	liveDetector             *speech.Detector
	livePending, liveASRWave []byte
	liveRecognizing          bool
	liveEpoch                uint64
	liveLastFrame            time.Time

	services        *app.Runtime
	voiceEnabled    bool
	recording       speech.Capture
	voicePhase      string
	captureRevision uint64

	chat        *chatUI
	transcript  transcriptState
	input       composer.Model
	suggestions suggestionState
	queued      string
	gateDraft   string
	events      chan api.Event
	hub         *api.GateHub
	cancel      context.CancelFunc
	ctx         context.Context
	// base spawns one context per run: an interrupted run's cancelled
	// context must never leak into the next turn.
	base  context.Context
	sess  *api.Session
	state runState
	gate  string
	steps int
	// maxSteps is the run's step budget for the meter: the loop default,
	// since the Interactive role never overrides it. Display only — the
	// loop enforces its own copy.
	maxSteps     int
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
	newSession func(task, stateFile string, images []string) (*api.Session, error)
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
	tokens int
	limit  int
	// tokensEst marks estimated numbers (streaming backends that report
	// no usage block); the meter prefixes "~" so estimates never pose
	// as measurements.
	tokensEst bool
	usage     usageState
	header    headerState

	// live accumulates streamed content chunks for the live answer
	// block; liveCut marks the 64KB truncation tail; lastLive gates
	// repaints (see livePaintInterval).
	live     string
	liveCut  bool
	lastLive time.Time
	// livePainted is the byte length painted at the last live repaint;
	// deltas before it are settled, after it pending. Cleared with live.
	livePainted int
	// firstToken stamps the first streamed chunk of the run: time to
	// first word beside elapsed time. Zero until streaming starts;
	// cleared with live at every turn boundary.
	firstToken time.Time
	// toolsUsed counts tool calls this run; filesTouched collects
	// observed changed paths. They reset with the step counter in startRun.
	toolsUsed    int
	filesTouched map[string]bool
	// overlays is the overlay stack (overlay.go); gates are its first
	// client. gstage/gateTool/gateResource describe the open permission
	// gate; note is the reject-note input; always remembers run-local
	// always-allow pairs confirmed in this process.
	overlays     []overlayFrame
	gstage       gateStage
	gateTool     string
	gateResource string
	note         textinput.Model
	alwaysMu     sync.Mutex
	always       map[[2]string]bool
	// dialog is the open modal, if any (dialog.go). While open it
	// replaces the transcript body and eats all keys but close/quit.
	dialog *dialog
	// sessions is the open session search popup, if any (sessions.go): a
	// title/history results with rename/delete. It overlays the existing
	// panes and owns its keys; like permission prompts it borrows
	// the note input for rename entry.
	sessions        *sessionsState
	nav             sessionsState
	navFocused      bool
	sidebarHidden   bool
	sessionIndex    map[string]cachedSession
	sessionRevision uint64
	pendingCommands []tea.Cmd
	drafts          map[string]chatDraft
	plan            bool
	mission         bool
	gateVP          viewport.Model
	// Status facts snapshotted from cfg at construction for /status:
	// backend/workspace identity and budgets the run was given.
	backendKind    string
	backendURL     string
	responseCap    int
	streaming      bool
	modelName      string
	wsRoot         string
	thinkBudget    int
	mcpCount       int
	policyRules    int
	policyDetails  []string
	speechMu       sync.Mutex
	speechCancel   context.CancelFunc
	speechRevision uint64
}

// gateEvent reshapes hub fields into the TUI's event: kind, prompt,
// and the permission call identity gates render from. A field dropped
// here never reaches handleEvent: the always-confirm scope went blank
// live for exactly this reason (tests inject events directly and never
// saw it).
func gateEvent(name string, fields map[string]any) api.Event {
	prompt, _ := fields["prompt"].(string)
	tool, _ := fields["tool"].(string)
	resource, _ := fields["resource"].(string)
	return api.Event{Name: name, Fields: map[string]any{
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
	env.OnToolResult = func(string, string) {}
	env.OnResult = func(agent.ToolResult) {}
	env.OnUsage = func(int, llm.Usage) {}
	env.OnFinding = nil
	env.OnNudge = nil
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

// Run executes the task under the TUI and returns the final answer.
// The terminal is restored on return, however the run ends.
func Run(ctx context.Context, cfg Config) (string, error) {
	if cfg.Chat != nil && (cfg.Env.WS != nil || cfg.Mission != nil || cfg.Plan || len(cfg.History) > 0 || cfg.StateFile != "") {
		return "", fmt.Errorf("tui: chat cannot use coding workspace, task history or workflow")
	}
	uiCtx, cancelUI := context.WithCancel(ctx)
	defer cancelUI()
	eventsCh := make(chan api.Event, 256)
	hub := api.NewGateHub(func(name string, fields map[string]any) {
		select {
		case eventsCh <- gateEvent(name, fields):
		case <-uiCtx.Done():
		}
	})
	runCtx, cancel := context.WithCancel(uiCtx)
	if cfg.Application != nil {
		setTheme(cfg.Application.Preferences.Theme)
	}
	m := &model{
		identityName: cfg.Env.IdentityName,
		identityUser: cfg.Env.UserName,
		events:       eventsCh,
		hub:          hub,
		cancel:       cancel,
		ctx:          runCtx,
		base:         uiCtx,
		started:      time.Now(),
		styles:       defaultStyles(),
		limit:        cfg.Env.ContextLimit,
		maxSteps:     envMaxSteps(cfg.Env),
		transcript:   newTranscript(),
		plan:         cfg.Plan,
		mission:      cfg.Mission != nil,
		stateFile:    cfg.StateFile,
		note:         newNote(),
		always:       map[[2]string]bool{},
		// Status facts come from cfg once: the TUI never re-reads Env.
		backendKind: cfg.Env.BackendKind,
		backendURL:  safeEndpoint(cfg.BackendURL),
		responseCap: cfg.Env.MaxTokens,
		streaming:   cfg.Env.Stream && cfg.Env.BackendKind != "kobold",
		modelName:   cfg.Env.Model,
		ws:          cfg.Env.WS,
		wsRoot:      wsRootOf(cfg.Env.WS),
		thinkBudget: cfg.Env.ReasoningBudget,
		mcpCount:    len(cfg.Env.MCPTools),
		policyRules: len(cfg.Env.Policy.Rules),
	}
	m.services = cfg.Runtime
	if m.services == nil {
		services := cfg.Chat
		if services == nil {
			services = cfg.Audio
		}
		m.services = app.NewRuntime(services)
	}
	defer func() {
		m.stopListening()
		m.cancel()
		if m.dialog != nil && m.dialog.form != nil && m.dialog.form.Workspace != nil && m.dialog.form.Workspace.SampleCancel != nil {
			m.dialog.form.Workspace.SampleCancel()
		}
		if cfg.Runtime == nil && m.services != nil {
			m.services.Close()
		}

	}()
	if len(cfg.History) > 0 && !cfg.Plan && readChatMode(filepath.Dir(cfg.StateFile)) == "Plan" {
		m.plan = true
	}
	if cfg.Chat != nil {
		if cfg.Chat.Directory == "" {
			return "", fmt.Errorf("tui: chat session directory required")
		}
		if _, err := chat.New(cfg.Chat.Client, cfg.Chat.Settings); err != nil {
			return "", err
		}
		m.chat = &chatUI{config: cfg.Chat}
		m.streaming = cfg.Chat.Stream
		m.ws = nil
		m.wsRoot = ""
		m.limit = cfg.Chat.Settings.ContextLimit
		m.responseCap = cfg.Chat.Settings.MaxTokens
	}
	m.application = cfg.Application
	m.audioConfig = cfg.Audio
	m.refreshNavigator()
	m.input = newInput()
	env := withoutPrintHooks(cfg.Env)
	env.Gate = nil
	env.GateContext = audit.ContextHook(cfg.AuditPath, "tui", m.contextGate(hub, cfg.Env.WS))
	if cfg.Chat == nil {
		m.newSession = func(task, stateFile string, images []string) (*api.Session, error) {
			return api.New(api.Config{
				Env:          env,
				Task:         task,
				Plan:         m.plan,
				StateFile:    stateFile,
				Verify:       cfg.Verify,
				Images:       images,
				Answer:       hub.Suspender(),
				ResumeAnswer: cfg.ResumeAnswer,
				OnEvent: func(ev api.Event) {
					select {
					case eventsCh <- ev:
					case <-uiCtx.Done():
					}
				},
			})
		}
	}
	for _, rule := range cfg.Env.Policy.Rules {
		m.policyDetails = append(m.policyDetails, fmt.Sprintf("%s · %s · %s", rule.Effect, rule.Tool, rule.Pattern))
	}
	m.application = cfg.Application
	m.audioConfig = cfg.Audio
	m.seedHistory = cfg.SeedHistory
	if cfg.SessionDirectory != "" {
		m.stateFile = filepath.Join(cfg.SessionDirectory, "state.json")
		if m.chat != nil {
			m.stateFile = filepath.Join(cfg.SessionDirectory, "conversation.json")
		}
		m.sessionTitle = workspace.ReadSessionTitle(cfg.SessionDirectory)
	}

	if m.audio() != nil && m.audio().Profile.Speech != nil && m.audio().Profile.Speech.Enabled {
		if err := m.enableVoice(); err != nil {
			return "", err
		}
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
			case eventsCh <- api.Event{Name: "_done", Fields: map[string]any{"answer": done.answer, "error": done.err}}:
			case <-uiCtx.Done():
			}
			return
		}
		prog.Send(msg)
	}
	sendEvent := func(ev api.Event) {
		select {
		case eventsCh <- ev:
		case <-uiCtx.Done():
		}
	}
	switch {
	case cfg.Chat != nil:
		m.state = stDone
		if strings.TrimSpace(cfg.Task) != "" {
			if err := m.startConversation(cfg.Task); err != nil {
				return "", err
			}
		} else {
			m.appendBlock(markerBlock("New conversation · Chat · no tools"))
			m.showGreeting()
			m.input.Focus()
		}
	case cfg.Mission != nil:
		m.appendBlock(userBlock(cfg.Task))
		m.startRun(func(runCtx context.Context) (string, error) {
			report := ""
			emitter := events.New(api.EventWriter(func(ev api.Event) {
				if ev.Name == "result" {
					report, _ = ev.Fields["report"].(string)
				}
				sendEvent(ev)
			}))
			missionEnv := env
			missionEnv.OnNudge = func(kind, text string) { api.EmitNudge(emitter, kind, text) }
			err := cfg.Mission(runCtx, missionEnv, hub.Suspender(), emitter)
			return report, err
		})
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
		m.appendBlock(markerBlock("New chat · type a task to begin\n/mode plan previews changes · /mode act executes them\n@path attaches workspace files or images"))
		m.input.Focus()
	}
	if len(cfg.SeedHistory) > 0 || cfg.SessionDirectory != "" {
		m.transcript.blocks = renderHistory(cfg.SeedHistory)
		m.restoreContext(cfg.SeedHistory)
		if m.chat != nil {
			c, err := chat.Restore(m.chat.config.Client, m.chat.config.Settings, DialogueProjection(cfg.SeedHistory))
			if err != nil {
				return "", err
			}
			m.chat.current = c
			if cfg.SessionDirectory != "" {
				m.stateFile = filepath.Join(cfg.SessionDirectory, "conversation.json")
				if err := m.chat.current.Save(m.stateFile); err != nil {
					return "", err
				}
			} else if err := m.createCarriedArchive(); err != nil {
				return "", err
			}
		}
		m.refreshContent()
	}
	if cfg.SessionDirectory != "" {
		m.stateFile = filepath.Join(cfg.SessionDirectory, "state.json")
		if m.chat != nil {
			m.stateFile = filepath.Join(cfg.SessionDirectory, "conversation.json")
		}
		if m.chat == nil {
			if err := agent.SaveState(m.stateFile, agentSeed(cfg.SeedHistory)); err != nil {
				return "", err
			}
		}
	}
	m.input.SetValue(cfg.InitialDraft)
	m.fitInput()
	m.fitBottom()
	if cfg.Application != nil && cfg.Application.Preferences.Voice == "live" {
		m.toggleListening()
	}
	if err := m.saveSessionSettings(); err != nil {
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
