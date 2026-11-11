// Package agent implements the Agent TUI screen and owns its conversation,
// execution, session browser, and input state. New constructs the screen;
// the application shell supplies shared controls and owns message routing.
package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/mcpmanager"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/userdirs"
)

// Config wires one Agent screen. Env carries the full harness (client,
// workspace, tools, policy, budgets); gate behavior comes from the TUI,
// so Env.Gate is overridden, never read.
type Config struct {
	Controls *screen.Controls // Shell-owned controls; nil creates screen-local controls.
	MCP      *mcpmanager.Manager
	// SessionsRoot is the saved-session library; empty uses the prepared profile
	// directory, or the user tasks directory when no profile is supplied.
	SessionsRoot     string
	SessionDirectory string
	Application      *settings.Application
	PreparedProfile  *app.PreparedProfile
	SeedHistory      []llm.Message
	InitialDraft     string
	InitialImages    []string
	Drafts           Drafts
	BackendURL       string
	AccountLabel     string
	Plan             bool
	History          []llm.Message
	Fork             bool // History seeds a new turn instead of recovering a paused run.
	ResumeAnswer     string

	Env       roles.Env
	Task      string
	StateFile string
	Verify    func(ctx context.Context) (output string, err error)
	// Images attaches pictures to the opening task (-image with
	// -tui); follow-up turns attach their own @paths per turn.
	Images []string
	// AuditPath, when set, appends permission decisions as JSONL.
	AuditPath string
}

// New constructs the Agent screen and restores its conversation and draft.
// The caller owns the launch context, shared controls, and terminal runtime.
func New(ctx context.Context, cfg Config) (model *Model, err error) {
	uiCtx, shutdown := context.WithCancel(ctx)
	m := newModel(uiCtx, cfg)
	m.shutdown = shutdown
	defer func() {
		if err != nil {
			m.Close()
		}
	}()

	if cfg.SessionDirectory != "" {
		if info, err := os.Stat(cfg.SessionDirectory); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("cannot open session directory %s", cfg.SessionDirectory)
		}

		m.stateFile = filepath.Join(cfg.SessionDirectory, "state.json")
		m.sessionTitle = conversation.ReadSessionTitle(cfg.SessionDirectory)
	}

	events := m.events
	m.send = func(msg tea.Msg) {
		if done, ok := msg.(doneMsg); ok {
			select {
			case events <- harness.Event{Name: "_done", Fields: map[string]any{"answer": done.answer, "error": done.err}}:
			case <-uiCtx.Done():
			}
		}
	}

	switch {
	case len(cfg.History) > 0:
		task := cfg.Task
		if strings.TrimSpace(task) == "" {
			task = "Continue the saved task."
		}

		sess, err := m.newSession(task, m.stateFile, cfg.Images)
		if err != nil {
			return nil, err
		}

		m.sess = sess
		m.transcript.blocks = append(m.transcript.blocks, renderHistory(cfg.History)...)
		if snapshot, err := conversation.Load(m.stateFile); err == nil {
			m.appendSavedRun(snapshot)
		}

		m.restoreContext(cfg.History)
		m.startRun(func(runCtx context.Context) (string, error) {
			if cfg.Fork {
				return sess.Continue(runCtx, cfg.History, task, cfg.Images...)
			}

			return sess.Resume(runCtx, cfg.History, task, cfg.Images...)
		})
	case strings.TrimSpace(cfg.Task) != "":
		if err := m.startInitial(cfg.Task, cfg.Images); err != nil {
			return nil, err
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
		if snapshot, err := conversation.Load(m.stateFile); err == nil {
			m.appendSavedRun(snapshot)
		}

		m.restoreContext(cfg.SeedHistory)
		m.refreshContent()
	}

	if cfg.SessionDirectory != "" {
		m.stateFile = filepath.Join(cfg.SessionDirectory, "state.json")
	}

	m.input.SetValue(cfg.InitialDraft)
	m.draftImages = append([]string(nil), cfg.InitialImages...)
	m.drafts = cfg.Drafts
	if _, ok := m.drafts[m.stateFile]; ok {
		m.restoreDraft()
	}

	m.fitInput()
	m.fitBottom()
	if err := m.savePreferences(); err != nil {
		return nil, err
	}

	return m, nil
}

// newModel assembles the terminal state and its production session factory.
func newModel(uiCtx context.Context, cfg Config) *Model {
	eventsCh := make(chan harness.Event, 256)
	hub := harness.NewGateHub(func(name string, fields map[string]any) {
		select {
		case eventsCh <- gateEvent(name, fields):
		case <-uiCtx.Done():
		}
	})
	// startRun replaces this with the cancel for the run it derives from
	// base. It exists so m.cancel is never nil: the gate and quit paths
	// call it, and a TUI sitting idle has not started a run yet.
	_, cancel := context.WithCancel(uiCtx)
	controls := cfg.Controls
	if controls == nil {
		controls = &screen.Controls{}
	}

	m := &Model{
		chrome:       controls,
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
		backendKind:  cfg.Env.BackendKind,
		accountLabel: cfg.AccountLabel,
		backendURL:   safeEndpoint(cfg.BackendURL),
		responseCap:  cfg.Env.MaxTokens,
		streaming:    cfg.Env.Stream,
		modelName:    cfg.Env.Model,
		ws:           cfg.Env.WS,
		wsRoot:       wsRootOf(cfg.Env.WS),
		mcpCount:     len(cfg.Env.MCPTools),
		policyRules:  len(cfg.Env.Policy.Rules),
	}
	if cfg.Env.MaxSteps == unlimitedSteps {
		m.maxStepsOverride = noStepLimit
	}

	m.application = cfg.Application
	m.config = cfg.PreparedProfile
	m.sessionsRoot = cfg.SessionsRoot
	if m.sessionsRoot == "" && cfg.Application != nil && cfg.PreparedProfile != nil {
		m.sessionsRoot = cfg.PreparedProfile.Directory
	}

	if m.sessionsRoot == "" {
		m.sessionsRoot = userdirs.Sub("tasks")
	}

	m.refreshSessions()
	m.input = newInput()

	env := cfg.Env
	env.Gate = permission.LogGate(cfg.AuditPath, "tui", m.contextGate(hub, cfg.Env.WS))
	m.baseEnv = env
	m.mcp = cfg.MCP
	if cfg.Application != nil {
		m.applyAppearance(cfg.Application.Preferences.Appearance)
		m.steering = cfg.Application.Preferences.Harness.Steering
	} else {
		m.applyAppearance(config.Appearance{})
	}

	m.newSession = func(task, stateFile string, images []string) (*harness.Session, error) {
		return harness.New(harness.Config{
			Env:          m.sessionEnv(),
			Task:         task,
			Plan:         m.plan,
			StateFile:    stateFile,
			Verify:       cfg.Verify,
			Images:       images,
			Answer:       hub.Suspender(),
			ResumeAnswer: cfg.ResumeAnswer,
			Events:       sessionEventQueue{ctx: uiCtx, queue: eventsCh},
		})
	}

	for _, rule := range cfg.Env.Policy.Rules {
		m.policyDetails = append(m.policyDetails, rule)
	}

	m.seedHistory = cfg.SeedHistory

	return m
}
