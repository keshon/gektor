package listfiles

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

type Tool struct{ WS *workspace.Workspace }

func (Tool) Name() string         { return "list_files" }
func (Tool) Mode() agent.ToolMode { return agent.Concurrent }

func (Tool) Description() string {
	return "List files and directories under a path, non-recursively."
}
func (Tool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {"path": {"type": "string"}},
		"required": ["path"]
	}`)
}

func (t Tool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	full, err := t.WS.Resolve(in.Path)
	if err != nil {
		return "", err
	}
	entries, err := t.WS.ReadDir(full)
	if err != nil {
		return "", err
	}
	out := ""
	for _, e := range entries {
		if e.IsDir() {
			out += e.Name() + "/\n"
		} else {
			out += e.Name() + "\n"
		}
	}
	return out, nil
}
