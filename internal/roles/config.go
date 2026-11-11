package roles

import (
	"context"
	"path/filepath"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tools"
)

// Shared run invariants; each role chooses only its capabilities and budget.
func (e Env) agentConfig(system, label, stateFile string) agent.Config {
	store := e.Outputs
	if store == nil {
		store = tools.NewOutputStore(e.statePath(stateFile))
	}
	project, skills := e.WS.InstructionSections()
	core := system + "\n\n" + prompts.Environment(e.WS.Root(), e.Vision) + e.browserContext()
	return agent.Config{
		Changes:        e.WS.NewChangeObserver(),
		Client:         e.Client,
		Inbox:          e.Inbox,
		OnInput:        e.OnInput,
		System:         core + project + skills + "\n\n" + e.Identity,
		SystemSections: []llm.PromptSection{{Name: "core_instructions", Text: core}, {Name: "project_instructions", Text: project}, {Name: "skill_metadata", Text: skills}, {Name: "identity", Text: "\n\n" + e.Identity}},
		OnPrompt:       e.OnPrompt,
		MaxTokens:      e.MaxTokens,
		Temperature:    e.Temperature,
		ContextLimit:   e.ContextLimit,
		StateFile:      e.statePath(stateFile),
		Workspace:      e.WS.Root(),
		OnStep:         e.onStep(label),
		OnResult:       e.OnResult,
		OnToolOutput:   e.OnToolOutput,
		OnToolFinished: e.OnToolFinished,
		OnUsage:        e.OnUsage,
		OnProgress:     e.OnProgress,
		OnNudge:        e.OnNudge,
		OnContext:      e.OnContext,
		ToolContext:    e.visionContext,
		Outputs:        store,
		Notifications:  e.Procs,
		Summarize:      true,
		OnLocal:        e.OnLocal,
		Paths:          tools.WorkspacePaths{WS: e.WS},
		Policy:         e.policy(),
		BeforeToolCall: e.Gate,
	}
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
