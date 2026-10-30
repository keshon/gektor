package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
)

// LoadSkill exposes library guides without widening file tools' workspace boundary.
type LoadSkill struct{ WS *workspace.Workspace }

func (LoadSkill) Name() string { return "load_skill" }
func (LoadSkill) Description() string {
	return "Load a discovered skill by ID, returning its complete instructions, base directory and digest. Optionally read a supporting text resource relative to its package. Discover IDs with system_info group=skills. Does not install or activate tools."
}
func (LoadSkill) Mode() agent.ToolMode { return agent.Concurrent }
func (LoadSkill) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"resource":{"type":"string","description":"Optional supporting text file relative to this skill's directory"}},"required":["id"],"additionalProperties":false}`)
}
func (t LoadSkill) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := rejectUnknownFields(args, "id", "resource"); err != nil {
		return "", err
	}
	var in struct {
		ID       string `json:"id"`
		Resource string `json:"resource"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if in.Resource != "" {
		skill, text, err := t.WS.LoadSkillResource(in.ID, in.Resource)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Skill %s resource %s\nBase directory: %s\n%s", skill.ID, in.Resource, skill.BaseDir, text), nil
	}
	skill, text, digest, err := t.WS.LoadSkill(in.ID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Skill %s\nSource: %s\nBase directory: %s\nSHA-256: %s\nUse load_skill with resource for supporting files. User requirements and tool permissions still apply.\n\n%s", skill.ID, skill.Path, skill.BaseDir, digest, text), nil
}

func (t LoadSkill) Resources(args json.RawMessage) ([]string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	return []string{in.ID}, nil
}

// Match the entire canonical result, including source, digest and body.
// A header, failed load, offload pointer or truncated body is never proof of
// delivery. Supporting resources remain ordinary reads.
func (t LoadSkill) ReuseInContext(args json.RawMessage, result string, history []llm.Message) string {
	var in struct {
		ID       string `json:"id"`
		Resource string `json:"resource"`
	}
	if json.Unmarshal(args, &in) != nil || in.ID == "" || in.Resource != "" {
		return result
	}
	header, body, complete := strings.Cut(result, "\nUse load_skill with resource for supporting files. User requirements and tool permissions still apply.\n\n")
	if !complete || !strings.HasPrefix(header, "Skill "+in.ID+"\nSource: ") || !strings.HasSuffix(header, fmt.Sprintf("SHA-256: %x", sha256.Sum256([]byte(body)))) {
		return result
	}
	calls := map[string]bool{}
	for _, msg := range history {
		if msg.Role == llm.RoleAssistant {
			for _, call := range msg.ToolCalls {
				var previous struct {
					ID       string `json:"id"`
					Resource string `json:"resource"`
				}
				calls[call.ID] = call.Name == t.Name() && call.InputError == "" && json.Unmarshal(call.Arguments, &previous) == nil && previous.ID == in.ID && previous.Resource == ""
			}
		}
		if msg.Role == llm.RoleTool && calls[msg.ToolCallID] && msg.ToolFailed != nil && !*msg.ToolFailed && msg.Content == result {
			return header + "\nComplete guide already present above in working context."
		}
	}
	return result
}
