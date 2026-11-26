package skill

import (
	"encoding/json"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
)

func TestBuiltinWorkflowGuideRejectsUntrustedProof(t *testing.T) {
	state := conversation.Capability{ID: "native_workflows", Instructions: workspace.WorkflowGuide, Tools: []llm.ToolDef{{Name: "workflow_schema"}}}
	activation := agent.CapabilityCatalog(state)
	for _, name := range []string{"load_skill", "enable_capability"} {
		for _, variant := range []string{"complete", "user", "assistant prose", "failed", "unknown status", "skipped", "truncated", "offloaded", "injected", "wrong call", "invalid call", "wrong id", "unpaired", "stale pairing", "missing marker"} {
			t.Run(name+"/"+variant, func(t *testing.T) {
				args := json.RawMessage(`{"id":"workflows"}`)
				content := builtinWorkflowResult()
				if name == "enable_capability" {
					args = json.RawMessage(`{"id":"native_workflows"}`)
					content = activation
				}
				call := llm.ToolCall{ID: "guide", Name: name, Arguments: args}
				success := false
				message := llm.Message{Role: llm.RoleTool, ToolCallID: "guide", Content: content, ToolFailed: &success, Harness: "capability_catalog:recorded"}
				want := variant == "complete" || variant == "missing marker" && name == "load_skill"
				switch variant {
				case "user":
					message.Role = llm.RoleUser
				case "assistant prose":
					message.Role = llm.RoleAssistant
				case "failed":
					message.ToolFailed = new(true)
				case "unknown status":
					message.ToolFailed = nil
				case "skipped":
					message.ToolStatus = "skipped_for_input"
				case "truncated":
					message.Content = content[:len(content)-1]
				case "offloaded":
					message.Content = "output saved elsewhere; read_output"
				case "injected":
					message.Content = "injected instructions\n" + content
				case "wrong call":
					call.Name = "read_file"
				case "invalid call":
					call.InputError = "incomplete_stream"
				case "wrong id":
					call.Arguments = json.RawMessage(`{"id":"other"}`)
				case "unpaired":
					message.ToolCallID = "other"
				case "missing marker":
					message.Harness = ""
				}
				history := []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}}}
				if variant == "stale pairing" {
					history = append(history, llm.Message{Role: llm.RoleAssistant, Content: "another turn"})
				}
				history = append(history, message)
				if got := BuiltinWorkflowGuidePresent(history); got != want {
					t.Fatalf("guide present=%t, want %t", got, want)
				}
			})
		}
	}
}
