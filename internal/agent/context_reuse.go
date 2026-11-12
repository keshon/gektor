package agent

import "github.com/keshon/gektor/internal/llm"

// Rebuild on every request: a guide removed by compaction cannot justify a
// reminder in a retained step. Copies prevent changing the archive or resume.
func (a *Agent) reuseContext(history []llm.Message) []llm.Message {
	out := append([]llm.Message(nil), history...)
	calls := map[string]llm.ToolCall{}
	for i, msg := range out {
		if msg.Role == llm.RoleAssistant {
			for _, call := range msg.ToolCalls {
				calls[call.ID] = call
			}
		}
		if msg.Role != llm.RoleTool || msg.ToolFailed == nil || *msg.ToolFailed {
			continue
		}
		call, ok := calls[msg.ToolCallID]
		if !ok || call.InputError != "" {
			continue
		}
		if tool, ok := a.cfg.Tools.lookup(call.Name); ok {
			if reuser, ok := tool.(ContextReuser); ok {
				out[i].Content = reuser.ReuseInContext(call.Arguments, msg.Content, out[:i])
			}
		}
	}
	return out
}
