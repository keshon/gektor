package gitadd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	commandrun "github.com/keshon/gektor/internal/tools/command"
	"github.com/keshon/gektor/internal/workspace"
)

type Tool struct{ WS *workspace.Workspace }

func (Tool) Name() string         { return "git_add" }
func (Tool) Mode() agent.ToolMode { return agent.Exclusive }
func (Tool) Description() string {
	return "Stage one literal workspace-relative file or directory, including deletions. Inspect status and diff first; stage only authorized changes."
}
func (Tool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1}},"required":["path"],"additionalProperties":false}`)
}
func (t Tool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if err := agent.RejectUnknownFields(args, "path"); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("path is required")
	}
	if _, err := t.WS.Resolve(in.Path); err != nil {
		return "", err
	}
	return commandrun.Git(ctx, t.WS.Root(), "add", "--", in.Path)
}
