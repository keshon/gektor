package roles

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"

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
		BeginChanges:   e.WS.ChangeObserver,
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
		ToolContext:    func(ctx context.Context) context.Context { return tools.VisionContext(store.Context(ctx), e.Vision) },
		LimitOutput:    tools.LimitOutput,
		Notifications:  e.Procs.Notifications,
		Summarize:      true,
		OnLocal:        e.OnLocal,
		Instructions: func(args json.RawMessage) (string, error) {
			var in struct {
				Path string `json:"path"`
				From string `json:"from"`
				To   string `json:"to"`
			}
			if json.Unmarshal(args, &in) != nil || e.WS == nil {
				return "", nil
			}
			var parts []string
			for _, path := range []string{in.Path, in.From, in.To} {
				if path == "" || llm.IsAttachment(path) {
					continue
				}
				text, err := e.WS.ScopedInstructions(path)
				if err != nil {
					return "", err
				}
				if text != "" && !slices.Contains(parts, text) {
					parts = append(parts, text)
				}
			}
			return strings.Join(parts, "\n"), nil
		},
		Policy: e.policy(),
		Resources: func(name string, args json.RawMessage) ([]string, error) {
			return tools.PathResources(e.WS, name, args)
		},
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
