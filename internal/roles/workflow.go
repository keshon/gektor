package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/capabilities/documents"
	doccontract "github.com/keshon/gektor/internal/capabilities/documents/contract"
	"github.com/keshon/gektor/internal/capabilities/files"
	filecontract "github.com/keshon/gektor/internal/capabilities/files/contract"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tools"
	toolbackground "github.com/keshon/gektor/internal/tools/background"
	toollistfiles "github.com/keshon/gektor/internal/tools/listfiles"
	toolreadfile "github.com/keshon/gektor/internal/tools/readfile"
	"github.com/keshon/gektor/internal/userdirs"
	workflow "github.com/keshon/gektor/internal/workflow/agenttools"
	"github.com/keshon/gektor/internal/workspace"
)

type authorValidation struct {
	*workflow.Tool
	save *workflow.Save
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
			if e == nil && path == v.save.Path {
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
		e.Procs = toolbackground.New()
	}
	cfg := e.executionConfig(prompts.WorkflowAuthor, stateFile)
	e.configureParent(&cfg, "workflow")
	project, _ := e.WS.InstructionSections()
	core := prompts.WorkflowAuthor + "\n\n" + workspace.WorkflowGuide + "\n\n" + prompts.Environment(e.WS.Root(), false)
	cfg.System = core + project + "\n\n" + e.Identity
	cfg.SystemSections = []llm.PromptSection{{Name: "core_instructions", Text: core}, {Name: "project_instructions", Text: project}, {Name: "identity", Text: "\n\n" + e.Identity}}
	storeDir := e.WorkflowDir
	if storeDir == "" {
		storeDir = userdirs.Sub("workflows")
	}
	service, err := app.NewWorkflow(e.WS, storeDir, e.policy(), e.Gate)
	if err != nil {
		return nil, err
	}
	tool := tools.NewWorkflow(service)
	tool.ValidateOnly = true
	validation := &authorValidation{Tool: tool}
	registry := validation.Registry
	save := &workflow.Save{WS: e.WS, Path: path, Registry: registry}
	validation.save = save
	cfg.Tools = agent.NewRegistry(
		workflow.Schema{WS: e.WS, StoreDir: storeDir, Registry: registry}, save,
		validation,
		toolreadfile.Tool{WS: e.WS}, toollistfiles.Tool{WS: e.WS},
		tools.DocumentOperation{Operation: doccontract.Operation{Service: documents.Service{WS: e.WS}, Kind: "docx_read"}},
		tools.DocumentOperation{Operation: doccontract.Operation{Service: documents.Service{WS: e.WS}, Kind: "xlsx_read"}},
		tools.DocumentOperation{Operation: doccontract.Operation{Service: documents.Service{WS: e.WS}, Kind: "pdf_read"}},

		tools.FileOperation{Operation: filecontract.Operation{Service: files.Service{WS: e.WS}, Kind: "archive_list"}},
		tools.NewImageOperation(e.WS, "inspect"), tools.NewImageOperation(e.WS, "preview"),
	)
	cfg.MutatingTools = []string{"workflow_save"}
	cfg.Verify = validation.verifySaved
	cfg.MaxSteps = e.MaxSteps
	if cfg.MaxSteps <= 0 || cfg.MaxSteps > 16 {
		cfg.MaxSteps = 16
	}
	cfg.RunTimeout = e.RunTimeout
	return agent.New(cfg), nil
}

// verifySaved checks the selected recipe again after the author finishes.
func (v *authorValidation) verifySaved(ctx context.Context) (string, error) {
	if !v.save.Saved {
		return "", fmt.Errorf("workflow author did not save a recipe")
	}
	if v.args == nil {
		return "saved recipe: " + filepath.ToSlash(v.save.Path), fmt.Errorf("saved workflow has not passed validation")
	}
	if _, err := v.Tool.Run(ctx, v.args); err != nil {
		return "saved recipe: " + filepath.ToSlash(v.save.Path), fmt.Errorf("saved workflow no longer validates: %w", err)
	}
	return "validated saved recipe: " + filepath.ToSlash(v.save.Path), nil
}
