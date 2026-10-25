package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/agent"
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
