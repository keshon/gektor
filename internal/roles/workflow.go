package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/keshon/gektor/internal/agent"
	workflow "github.com/keshon/gektor/internal/automation/agenttools"
	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/integrations/pixelita"
	imageagent "github.com/keshon/gektor/internal/integrations/pixelita/agenttools"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tools"
)

type authorValidation struct {
	*workflow.Tool
	path string
	args json.RawMessage
}

func (*authorValidation) Mode() agent.ToolMode { return agent.Exclusive }
func (v *authorValidation) Run(ctx context.Context, args json.RawMessage) (string, error) {
	out, err := v.Tool.Run(ctx, args)
	if err == nil {
		var in struct {
			Path string `json:"recipe_path"`
		}
		if json.Unmarshal(args, &in) == nil {
			path, e := v.WS.Relative(in.Path)
			if e == nil && path == v.path {
				v.args = append(json.RawMessage(nil), args...)
			}
		}
	}
	return out, err
}

// WorkflowAuthor can inspect sources and write one operator-selected recipe.
// Its registry has no execution, extension activation or delegation tools.
func WorkflowAuthor(e Env, recipePath, stateFile string) (*agent.Agent, error) {
	if e.WS == nil {
		return nil, fmt.Errorf("workflow author needs a workspace")
	}
	path, err := e.WS.Relative(recipePath)
	if err != nil {
		return nil, err
	}
	if path == "." {
		return nil, fmt.Errorf("recipe path must name a file")
	}
	if e.Procs == nil {
		e.Procs = tools.NewBackgroundProcesses()
	}
	cfg := e.agentConfig(prompts.WorkflowAuthor, "workflow", stateFile)
	project, _ := e.WS.InstructionSections()
	core := prompts.WorkflowAuthor + "\n\n" + prompts.Environment(e.WS.Root(), false)
	cfg.System = core + project + "\n\n" + e.Identity
	cfg.SystemSections = []llm.PromptSection{{Name: "core_instructions", Text: core}, {Name: "project_instructions", Text: project}, {Name: "identity", Text: "\n\n" + e.Identity}}
	save := &workflow.Save{WS: e.WS, Path: path}
	validation := &authorValidation{Tool: tools.NewWorkflow(e.WS, true, e.policy(), e.ResolveGate()), path: path}
	storeDir := e.WorkflowDir
	if storeDir == "" {
		storeDir = gektor.Sub("workflows")
	}
	validation.StoreDir = storeDir
	cfg.Tools = agent.NewRegistry(
		workflow.Schema{WS: e.WS, StoreDir: storeDir}, save,
		validation,
		tools.ReadFile{WS: e.WS}, tools.ListFiles{WS: e.WS},
		&imageagent.Tool{Operation: pixelita.Operation{WS: e.WS, Operation: "inspect"}}, &imageagent.Tool{Operation: pixelita.Operation{WS: e.WS, Operation: "preview"}},
	)
	cfg.Resources = func(name string, args json.RawMessage) ([]string, error) {
		if name == "workflow_save" {
			return []string{filepath.ToSlash(path)}, nil
		}
		return tools.PathResources(e.WS, name, args)
	}
	cfg.MutatingTools = []string{"workflow_save"}
	cfg.Verify = func(ctx context.Context) (string, error) {
		if !save.Saved {
			return "", fmt.Errorf("workflow author did not save a recipe")
		}
		if validation.args == nil {
			return "saved recipe: " + filepath.ToSlash(path), fmt.Errorf("saved workflow has not passed validation")
		}
		if _, err := validation.Tool.Run(ctx, validation.args); err != nil {
			return "saved recipe: " + filepath.ToSlash(path), fmt.Errorf("saved workflow no longer validates: %w", err)
		}
		return "validated saved recipe: " + filepath.ToSlash(path), nil
	}
	cfg.MaxSteps = e.MaxSteps
	if cfg.MaxSteps <= 0 || cfg.MaxSteps > 16 {
		cfg.MaxSteps = 16
	}
	cfg.RunTimeout = e.RunTimeout
	cfg.Stream, cfg.OnDelta, cfg.OnReasoning = e.Stream, e.OnDelta, e.OnReasoning
	return agent.New(cfg), nil
}
