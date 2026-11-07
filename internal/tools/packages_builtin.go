package tools

import (
	"github.com/keshon/gektor/internal/agent"
	workflow "github.com/keshon/gektor/internal/automation/agenttools"
	"github.com/keshon/gektor/internal/integrations/pixelita"
	imageagent "github.com/keshon/gektor/internal/integrations/pixelita/agenttools"
	"github.com/keshon/gektor/internal/workspace"
)

func nativeInstructions(id string) string {
	if id == "native_workflows" {
		return workspace.WorkflowGuide
	}
	if id == "native_images" {
		return "These native tools call Pixelita in-process; CLI setup/discovery is unnecessary. unsupported_metadata is a shared engine limitation: do not retry via CLI, keep or apply. Report the restriction and preserve profiles. Claim source preservation only from matching source hashes."
	}
	return ""
}

// Factories construct fresh native tools; no entry captures session state.
func builtinPackages() []packageDefinition {
	return []packageDefinition{
		{"core", "Session support", Always, []toolEntry{
			{func(d packageDependencies) agent.Tool {
				info := SystemInfo{WS: d.WS, BrowserTools: BrowserToolNames(d.Extra)}
				for _, t := range d.Extra {
					if v, ok := t.(DiscoverCapabilities); ok {
						info.Capabilities = v.Catalog
					}
					if v, ok := t.(*EnableBrowser); ok {
						info.Browser = v
					}
				}
				return info
			}, true},
			{func(d packageDependencies) agent.Tool { return LoadSkill{WS: d.WS} }, true},
			{func(d packageDependencies) agent.Tool { return ReadOutput{} }, true},
			{func(d packageDependencies) agent.Tool { return &Todo{} }, false},
		}},
		{"workspace", "Workspace files", Always, []toolEntry{
			{func(d packageDependencies) agent.Tool { return ReadFile{WS: d.WS, Vision: d.Vision} }, true},
			{func(d packageDependencies) agent.Tool { return Checked(WriteFile{WS: d.WS}, d.WS, d.Report, d.Rules) }, false},
			{func(d packageDependencies) agent.Tool { return Checked(PatchFile{WS: d.WS}, d.WS, d.Report, d.Rules) }, false},
			{func(d packageDependencies) agent.Tool { return ListFiles{WS: d.WS} }, true},
			{func(d packageDependencies) agent.Tool { return GrepFiles{WS: d.WS} }, true},
			{func(d packageDependencies) agent.Tool { return MoveFile{WS: d.WS} }, false},
		}},
		{"execution", "Run commands", Always, []toolEntry{
			{func(d packageDependencies) agent.Tool { return RunShell{WS: d.WS} }, false},
			{func(d packageDependencies) agent.Tool { return RunProgram{WS: d.WS} }, false},
		}},
		{"processes", "Background processes", Always, []toolEntry{
			{func(d packageDependencies) agent.Tool { return StartBackground{WS: d.WS, Procs: d.Procs} }, false},
			{func(d packageDependencies) agent.Tool { return StartProgram{WS: d.WS, Procs: d.Procs} }, false},
			{func(d packageDependencies) agent.Tool { return CheckBackground{Procs: d.Procs} }, true},
			{func(d packageDependencies) agent.Tool { return WaitBackground{Procs: d.Procs} }, true},
			{func(d packageDependencies) agent.Tool { return StopBackground{Procs: d.Procs} }, false},
		}},
		{"git", "Git operations", Always, []toolEntry{
			{func(d packageDependencies) agent.Tool { return Git{WS: d.WS} }, true},
			{func(d packageDependencies) agent.Tool { return GitAdd{WS: d.WS} }, false},
			{func(d packageDependencies) agent.Tool { return GitCommit{WS: d.WS} }, false},
		}},
		{NativeWeb, "Fetch URLs and search the web", Eager, []toolEntry{
			{func(d packageDependencies) agent.Tool { return CheckURL{} }, true},
			{func(d packageDependencies) agent.Tool { return Webfetch{} }, true},
			{func(d packageDependencies) agent.Tool { return FetchRaw{} }, true},
			{func(d packageDependencies) agent.Tool { return SearchWeb{WS: d.WS} }, true},
		}},
		{"native_images", "Inspect, preview, write separate image variants and compare fidelity", Lazy, []toolEntry{
			{func(d packageDependencies) agent.Tool {
				return &imageagent.Tool{Operation: pixelita.Operation{WS: d.WS, Operation: "inspect"}}
			}, true},
			{func(d packageDependencies) agent.Tool {
				return &imageagent.Tool{Operation: pixelita.Operation{WS: d.WS, Operation: "preview"}}
			}, true},
			{func(d packageDependencies) agent.Tool {
				return &imageagent.Tool{Operation: pixelita.Operation{WS: d.WS, Operation: "variants"}}
			}, false},
			{func(d packageDependencies) agent.Tool {
				return &imageagent.Tool{Operation: pixelita.Operation{WS: d.WS, Operation: "compare"}}
			}, true},
		}},
		{"native_workflows", "Create, validate and run workflow recipes and nodes; native cron/schedule triggers, run inspection and structured records. Enable, then call workflow_schema.", Lazy, []toolEntry{
			{func(d packageDependencies) agent.Tool { return &workflow.Schema{WS: d.WS} }, true},
			{func(d packageDependencies) agent.Tool { return &workflow.Tool{WS: d.WS, ValidateOnly: true} }, true},
			{func(d packageDependencies) agent.Tool { return &workflow.Tool{WS: d.WS} }, false},
			{func(d packageDependencies) agent.Tool { return &workflow.Inspect{WS: d.WS} }, true},
			{func(d packageDependencies) agent.Tool { return &workflow.Schedule{WS: d.WS, List: true} }, true},
			{func(d packageDependencies) agent.Tool { return &workflow.Schedule{WS: d.WS} }, false},
			{func(d packageDependencies) agent.Tool { return &workflow.Status{WS: d.WS} }, true},
			{func(d packageDependencies) agent.Tool { return &workflow.Data{WS: d.WS} }, true},
			{func(d packageDependencies) agent.Tool { return &workflow.Data{WS: d.WS, Write: true} }, false},
		}},
	}
}
