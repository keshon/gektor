package extension

import (
	"encoding/json"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	toolskill "github.com/keshon/gektor/internal/tools/skill"
	"github.com/keshon/gektor/internal/workspace"
)

// Activation still publishes tools and archives its full result. Only repeated
// built-in guidance is shortened in the model's per-request history projection.
func (t EnableCapability) ReuseInContext(args json.RawMessage, result string, history []llm.Message) string {
	var in struct {
		ID string `json:"id"`
	}
	err := json.Unmarshal(args, &in)
	if err != nil || in.ID != "native_workflows" || t.Catalog == nil {
		return result
	}
	err = agent.RejectUnknownFields(args, "id")
	if err != nil || !toolskill.BuiltinWorkflowGuidePresent(history) {
		return result
	}
	for _, state := range t.Catalog.Save() {
		if state.ID == in.ID && !state.Unloaded && state.Instructions == workspace.WorkflowGuide && result == activationText(state) {
			return strings.TrimSuffix(result, state.Instructions) + "Built-in workflows guide already present above in working context."
		}
	}
	return result
}
