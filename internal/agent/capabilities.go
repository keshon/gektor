package agent

import (
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

// Schemas come from the registry; only operational instructions need restoration.
func (a *Agent) withCapabilityInstructions(history []llm.Message) []llm.Message {
	if a.cfg.Catalog == nil {
		return history
	}
	for _, state := range a.cfg.Catalog.Save() {
		if state.Unloaded {
			projected := append([]llm.Message(nil), history...)
			marker := "capability_catalog:" + state.Fingerprint
			for i, message := range projected {
				if message.Harness == marker {
					projected[i].Content = "Toolset " + state.ID + " is not loaded in this conversation."
				}
			}
			history = projected
			continue
		}
		if strings.TrimSpace(state.Instructions) == "" || strings.Contains(a.cfg.System, state.Instructions) {
			continue
		}
		marker := "capability_catalog:" + state.Fingerprint
		present := false
		for _, message := range history {
			if message.Harness == marker {
				present = true
				break
			}
		}
		if !present {
			history = append(history, llm.Message{Role: llm.RoleUser, Harness: marker, Content: harnessPrefix + CapabilityCatalog(state)})
		}
	}
	return history
}

func CapabilityCatalog(state conversation.Capability) string {
	names := make([]string, 0, len(state.Tools))
	for _, tool := range state.Tools {
		names = append(names, tool.Name)
	}
	tools := strings.Join(names, ", ")
	return fmt.Sprintf("Capability %s enabled for this session. Call these tools directly using their tool schemas: %s.\n%s", state.ID, tools, state.Instructions)
}
