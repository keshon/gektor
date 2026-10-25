package agent

import (
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/llm"
)

// A compacted catalog still needs its operational instructions. Append only
// missing blocks, preserving the system prefix and ordinary append-only turns.
func (a *Agent) withCapabilityInstructions(history []llm.Message) []llm.Message {
	if a.cfg.SaveCapabilities == nil {
		return history
	}
	for _, state := range a.cfg.SaveCapabilities() {
		if strings.TrimSpace(state.Instructions) == "" {
			continue
		}
		present := false
		for _, message := range history {
			if strings.Contains(message.Content, state.Instructions) {
				present = true
				break
			}
		}
		if !present {
			history = a.nudge(history, NudgeRecovery, fmt.Sprintf("Active capability %s instructions; current tool permissions still apply:\n%s", state.ID, state.Instructions))
		}
	}
	return history
}
