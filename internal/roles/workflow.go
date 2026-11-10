package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/capabilities/documents"
	doccontract "github.com/keshon/gektor/internal/capabilities/documents/contract"
	"github.com/keshon/gektor/internal/capabilities/files"
	filecontract "github.com/keshon/gektor/internal/capabilities/files/contract"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/userdirs"
	workflow "github.com/keshon/gektor/internal/workflow/agenttools"
	"github.com/keshon/gektor/internal/workspace"
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
	core := prompts.WorkflowAuthor + "\n\n" + workspace.WorkflowGuide + "\n\n" + prompts.Environment(e.WS.Root(), false)
	cfg.System = core + project + "\n\n" + e.Identity
	cfg.SystemSections = []llm.PromptSection{{Name: "core_instructions", Text: core}, {Name: "project_instructions", Text: project}, {Name: "identity", Text: "\n\n" + e.Identity}}
	storeDir := e.WorkflowDir
	if storeDir == "" {
		storeDir = userdirs.Sub("workflows")
	}
	validation := &authorValidation{Tool: tools.NewWorkflow(tools.Dependencies{WS: e.WS, Policy: e.policy(), Gate: e.Gate, StoreDir: storeDir}, true), path: path}
	registry := validation.Registry
	save := &workflow.Save{WS: e.WS, Path: path, Registry: registry}
	cfg.Tools = agent.NewRegistry(
		workflow.Schema{WS: e.WS, StoreDir: storeDir, Registry: registry}, save,
		validation,
		tools.ReadFile{WS: e.WS}, tools.ListFiles{WS: e.WS},
		tools.DocumentOperation{Operation: doccontract.Operation{Service: documents.Service{WS: e.WS}, Kind: "docx_read"}},
		tools.DocumentOperation{Operation: doccontract.Operation{Service: documents.Service{WS: e.WS}, Kind: "xlsx_read"}},
		tools.DocumentOperation{Operation: doccontract.Operation{Service: documents.Service{WS: e.WS}, Kind: "pdf_read"}},

		tools.FileOperation{Operation: filecontract.Operation{Service: files.Service{WS: e.WS}, Kind: "archive_list"}},
		tools.NewImageOperation(e.WS, "inspect"), tools.NewImageOperation(e.WS, "preview"),
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
