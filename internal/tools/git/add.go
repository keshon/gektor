package git

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

type Add struct{ WS *workspace.Workspace }

func (Add) Name() string         { return "git_add" }
func (Add) Mode() agent.ToolMode { return agent.Exclusive }
func (Add) Description() string {
	return "Stage one literal workspace-relative file or directory, including deletions. Inspect status and diff first; stage only authorized changes."
}
func (Add) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1}},"required":["path"],"additionalProperties":false}`)
}
func (t Add) Run(ctx context.Context, args json.RawMessage) (string, error) {
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
	return run(ctx, t.WS.Root(), "add", "--", in.Path)
}
