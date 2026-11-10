package agent

import (
	"strings"

	"github.com/keshon/gektor/internal/llm"
)

// StartupContext previews a fresh run without tools, network or state writes.
func (a *Agent) StartupContext(task string) llm.ContextReport {
	history := a.withDirective([]llm.Message{{Role: llm.RoleSystem, Content: a.cfg.System}, {Role: llm.RoleUser, Content: task}})
	return llm.AnalyzeContext(llm.ChatRequest{Messages: llm.ModelMessages(history), Tools: a.previewTools()}, a.cfg.SystemSections)
}

func (a *Agent) previewTools() []llm.ToolDef {
	if a.cfg.PreviewTools != nil {
		return a.cfg.PreviewTools()
	}
	return a.cfg.Tools.ModelDefs()
}

// Identity is derived from the saved archive, so compaction cannot substitute
// an assistant note or a tool image for the current operator request.
func (a *Agent) requestIdentity(req *llm.ChatRequest) {
	req.UserRequest, req.UserRequestCount, req.RunDirective = "", 0, a.cfg.Directive
	for i, message := range a.snapshot.History {
		// Older transcripts did not tag tool-image turns with Harness metadata.
		if i > 0 && a.snapshot.History[i-1].Role == llm.RoleTool && len(message.Images) > 0 && message.Content == pictureNote(message.Images) {
			continue
		}
		if message.Role == llm.RoleUser && message.Harness == "" && !strings.HasPrefix(message.Content, harnessPrefix) {
			req.UserRequest = message.Content
			req.UserRequestCount++
		}
	}
}

func (a *Agent) modelRequest(history []llm.Message) llm.ChatRequest {
	history = a.withCapabilityInstructions(history)
	req := llm.ChatRequest{Messages: a.reuseContext(history), Tools: a.cfg.Tools.ModelDefs()}
	a.requestIdentity(&req)
	return req
}
