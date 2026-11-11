package agent

import (
	"strings"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
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

// situationHints names, per nudge situation, the tools that could help.
// A situation whose tools are all absent yields nothing.
var situationHints = map[string][]string{"truncated": {"write_file", "patch_file"}}

var situationPrompts = map[string]string{"truncated": prompts.WriteHint}

// hintResolver answers from a registry which of a situation's tools are
// actually registered. It is the default Config.ToolHint; a caller that
// wants different advice replaces it.
func hintResolver(reg *Registry) func(string) string {
	return func(situation string) string {
		if !reg.HasAny(situationHints[situation]...) {
			return ""
		}
		return situationPrompts[situation]
	}
}

// hint asks the run's owner which registered tool suits this situation.
// Empty when nothing does, or when the run wired no resolver.
func (a *Agent) hint(situation string) string {
	if a.cfg.ToolHint == nil {
		return ""
	}
	if line := strings.TrimSpace(a.cfg.ToolHint(situation)); line != "" {
		return " " + line
	}
	return ""
}

// withDirective keeps the role's stance at the end of the conversation. A
// stance is a harness turn rather than part of System because System is the
// prompt's first message: changing it when the role changes makes a caching
// backend re-read the entire conversation. The turn is only appended when the
// conversation ended in a different stance, so resuming in the same role adds
// nothing, and calling this after compaction restores a stance the compaction
// dropped.
func (a *Agent) withDirective(history []llm.Message) []llm.Message {
	if a.cfg.Directive == "" {
		return history
	}
	mine := harnessPrefix + a.cfg.Directive
	other := ""
	if a.cfg.Supersedes != "" {
		other = harnessPrefix + a.cfg.Supersedes
	}
	last := ""
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != llm.RoleUser {
			continue
		}
		if history[i].Content == mine {
			last = mine
			break
		}
		if other != "" && history[i].Content == other {
			last = other
			break
		}
	}
	if last == mine {
		return history
	}
	return append(history, llm.Message{Role: llm.RoleUser, Content: mine})
}
