package roles

import (
	"context"
	"path/filepath"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tools"
)

// Execution settings shared by all roles, without parent input or event routing.
func (e Env) executionConfig(system, stateFile string) agent.Config {
	store := e.Outputs
	if store == nil {
		store = tools.NewOutputStore(e.statePath(stateFile))
	}

	project, skills := e.WS.InstructionSections()
	core := system + "\n\n" + prompts.Environment(e.WS.Root(), e.Vision) + e.browserContext()

	return agent.Config{
		System: core + project + skills + "\n\n" + e.Identity,
		SystemSections: []llm.PromptSection{
			{Name: "core_instructions", Text: core},
			{Name: "project_instructions", Text: project},
			{Name: "skill_metadata", Text: skills},
			{Name: "identity", Text: "\n\n" + e.Identity},
		},

		Client:       e.Client,
		MaxTokens:    e.MaxTokens,
		Temperature:  e.Temperature,
		ContextLimit: e.ContextLimit,
		StateFile:    e.statePath(stateFile),
		Workspace:    e.WS.Root(),

		Changes:        e.WS.NewChangeObserver(),
		ToolContext:    e.visionContext,
		Outputs:        store,
		Summarize:      true,
		Paths:          tools.WorkspacePaths{WS: e.WS},
		Policy:         e.policy(),
		BeforeToolCall: e.Gate,
	}
}

// Parent runs consume operator input and report directly to the frontend.
// Children attach only their separate event route in observeSubagent.
func (e Env) configureParent(cfg *agent.Config, label string) {
	cfg.Inbox = e.Inbox
	cfg.OnInput = e.OnInput
	cfg.Notifications = e.Procs

	cfg.OnStep = e.onStep(label)
	cfg.OnResult = e.OnResult
	cfg.OnToolOutput = e.OnToolOutput
	cfg.OnToolFinished = e.OnToolFinished
	cfg.OnUsage = e.OnUsage
	cfg.OnProgress = e.OnProgress
	cfg.OnPrompt = e.OnPrompt
	cfg.OnNudge = e.OnNudge
	cfg.OnContext = e.OnContext
	cfg.OnLocal = e.OnLocal

	cfg.Stream = e.Stream
	cfg.OnDelta = e.OnDelta
	cfg.OnReasoning = e.OnReasoning
}

func (e Env) statePath(name string) string {
	switch {
	case name == "":
		return ""
	case e.StateDir == "":
		return name
	default:
		return filepath.Join(e.StateDir, name)
	}
}

// visionContext supplies the probed model capability to image-producing adapters.
func (e Env) visionContext(ctx context.Context) context.Context {
	return tools.VisionContext(ctx, e.Vision)
}
