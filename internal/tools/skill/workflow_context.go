package skill

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
)

func builtinWorkflowResult() string {
	skill := workspace.Skill{ID: "workflows", Path: "builtin:workflows/SKILL.md", BaseDir: "builtin:workflows"}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(workspace.WorkflowGuide)))
	return guideResult(skill, workspace.WorkflowGuide, digest)
}

// BuiltinWorkflowGuidePresent proves complete delivery in the retained context.
// Only successful paired tool results count; reminders and custom skills do not.
func BuiltinWorkflowGuidePresent(history []llm.Message) bool {
	calls := map[string]llm.ToolCall{}
	for _, message := range history {
		if message.Role == llm.RoleAssistant {
			calls = make(map[string]llm.ToolCall, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				calls[call.ID] = call
			}
			continue
		}
		if message.Role != llm.RoleTool {
			continue
		}
		call, paired := calls[message.ToolCallID]
		delete(calls, message.ToolCallID)
		if !paired || call.InputError != "" || message.ToolFailed == nil || *message.ToolFailed || (message.ToolStatus != "" && message.ToolStatus != "success") {
			continue
		}
		var in struct {
			ID       string `json:"id"`
			Resource string `json:"resource"`
		}
		err := json.Unmarshal(call.Arguments, &in)
		if err != nil {
			continue
		}
		switch call.Name {
		case "load_skill":
			err = agent.RejectUnknownFields(call.Arguments, "id", "resource")
			if err == nil && in.ID == "workflows" && in.Resource == "" && message.Content == builtinWorkflowResult() {
				return true
			}
		case "enable_capability":
			err = agent.RejectUnknownFields(call.Arguments, "id")
			if err != nil || in.ID != "native_workflows" || !strings.HasPrefix(message.Harness, "capability_catalog:") {
				continue
			}
			header, body, complete := strings.Cut(message.Content, "\n")
			const prefix = "Capability native_workflows enabled for this session. Call these tools directly using their tool schemas: "
			if !complete || body != workspace.WorkflowGuide || !strings.HasPrefix(header, prefix) || !strings.HasSuffix(header, ".") {
				continue
			}
			// The only variable header field is a comma-separated tool-name list.
			// Reject appended prose even if it retains an identical guide body.
			names := strings.TrimSuffix(strings.TrimPrefix(header, prefix), ".")
			valid := true
			for _, name := range strings.Split(names, ", ") {
				if name == "" || strings.IndexFunc(name, func(r rune) bool {
					return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
				}) >= 0 {
					valid = false
					break
				}
			}
			if valid {
				return true
			}
		}
	}
	return false
}
