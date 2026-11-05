package tools

import "github.com/keshon/gektor/internal/agent"

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
	}
}
