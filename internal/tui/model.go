package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/app"
	agentui "github.com/keshon/gektor/internal/tui/agent"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/tui/workflow"
)

type activeScreen int

const (
	agentScreen activeScreen = iota
	workflowScreen
)

// Model owns application navigation, shared controls, and the lifetime of its
// Agent and Workflow screens. It never reaches into either screen's private state.
type Model struct {
	form pendingForm

	profile           *app.PreparedProfile
	application       *settings.Application
	modeChange        *ModeChange
	overlayScreen     activeScreen
	commands          []tea.Cmd
	agent             *agentui.Model
	workflows         *workflow.Model
	chrome            *screen.Controls
	active            activeScreen
	launch            Config
	base              context.Context
	startWorkflows    bool
	termW, termH      int
	outlined          bool
	quit, pendingQuit bool
}

func newModel(ctx context.Context, cfg Config) (*Model, error) {
	if cfg.Application != nil {
		screen.SetTheme(cfg.Application.Preferences.Theme)
	}
	controls := &screen.Controls{}
	agentCfg := cfg.AgentConfig
	agentCfg.Controls = controls
	conversation, err := agentui.New(ctx, agentCfg)
	if err != nil {
		return nil, err
	}
	m := &Model{
		agent:          conversation,
		chrome:         controls,
		launch:         cfg,
		profile:        cfg.PreparedProfile,
		application:    cfg.Application,
		base:           ctx,
		outlined:       conversation.Appearance().Frame == "outlined",
		startWorkflows: cfg.StartScreen == "workflows",
	}

	m.commands = append(m.commands, m.settleControls()...)
	return m, nil
}

// Close releases both screens, including their background operations.
func (m *Model) Close() {
	if m.workflows != nil {
		m.workflows.Close()
	}
	m.agent.Close()
}

// Init starts event delivery after applying the application's initial screen.
func (m *Model) Init() tea.Cmd {
	if m.startWorkflows {
		m.startWorkflows = false
		m.openWorkflows()
	}
	m.commands = append(m.commands, m.settleControls()...)
	return tea.Batch(m.agent.Init(), m.takeCommands())
}

func (m *Model) workflowActive() bool { return m.active == workflowScreen && m.workflows != nil }
func (m *Model) openWorkflows() {
	if !m.agent.Status().Idle {
		m.chrome.Open("Workflows", []string{"Finish or stop the agent turn before opening the editor."})
		return
	}
	if m.workflows == nil {
		env := m.launch.AgentConfig.Env
		if env.WS == nil {
			m.chrome.Open("Workflows", []string{"Select a workspace first."})
			return
		}
		cfg := workflow.Config{
			WS:           env.WS,
			Client:       env.Client,
			Policy:       env.Policy,
			MaxTokens:    env.MaxTokens,
			ContextLimit: env.ContextLimit,
			Temperature:  env.Temperature,
		}
		child, err := workflow.New(m.base, cfg, m.chrome, m.workflowSurface())
		if err != nil {
			m.chrome.Open("Workflows", []string{err.Error()})
			return
		}
		m.workflows = child
	}
	m.agent.Hide()
	m.workflows.Activate()
	m.active = workflowScreen
}
func (m *Model) showAgent() {
	if m.workflows != nil {
		if m.workflows.State().Busy {
			return
		}
		m.workflows.Hide()
	}
	m.active = agentScreen
	m.agent.Activate()
}
func (m *Model) workflowCommand(action string) tea.Cmd {
	if !m.workflowActive() {
		m.openWorkflows()
	}
	if !m.workflowActive() {
		return nil
	}
	return m.workflows.Command(action)
}
func (m *Model) requestQuit() (tea.Model, tea.Cmd) {
	if w := m.workflows; w != nil {
		if !w.PrepareQuit() {
			return m, nil
		}
		if w.Dirty() {
			m.openForm(screen.Form{
				Title:      "Quit with unsaved workflow?",
				Hint:       "Return to Workflows and save, or discard the draft.",
				ApplyLabel: "Quit",
				Fields:     []form.Field{{Key: "decision", Label: "Type discard to quit"}},
			}, func(values map[string]string) error {
				if values["decision"] != "discard" {
					return fmt.Errorf("type discard, or Esc to keep editing")
				}
				m.workflows.Close()
				m.workflows = nil
				m.active = agentScreen
				m.pendingQuit = true
				return nil
			})
			return m, nil
		}
	}
	if !m.agent.PrepareQuit() {
		return m, nil
	}
	m.quit = true
	return m, tea.Quit
}
func (m *Model) takeCommands() tea.Cmd {
	commands := append(m.commands, m.settingsCommands()...)
	m.commands = nil
	if !m.workflowActive() {
		commands = append(commands, m.agent.Refresh())
	}
	if m.workflows != nil {
		commands = append(commands, m.workflows.TakeCommands()...)
	}
	return tea.Batch(commands...)
}
