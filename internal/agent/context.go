package agent

import "github.com/keshon/gektor/internal/llm"

// StartupContext previews a fresh run without tools, network or state writes.
func (a *Agent) StartupContext(task string) llm.ContextReport {
	history := a.withDirective([]llm.Message{{Role: llm.RoleSystem, Content: a.cfg.System}, {Role: llm.RoleUser, Content: task}})
	return llm.AnalyzeContext(llm.ChatRequest{Messages: llm.ModelMessages(history), Tools: a.previewTools()}, a.cfg.SystemSections)
}

func (a *Agent) previewTools() []llm.ToolDef {
	if a.cfg.PreviewTools != nil {
		return a.cfg.PreviewTools()
	}
	return a.cfg.Tools.Defs()
}
