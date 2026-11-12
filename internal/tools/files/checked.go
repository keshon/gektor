package files

import (
	"context"
	"encoding/json"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/workspace"
)

// checked observes successful path-based writes. Reporter owns reading, limits,
// checks and deduplication; observation never changes the tool's result.
type checked struct {
	inner  agent.Tool
	ws     *workspace.Workspace
	report *checks.Reporter
}

func Checked(inner agent.Tool, ws *workspace.Workspace, report *checks.Reporter) agent.Tool {
	if report == nil || ws == nil {
		return inner
	}
	return &checked{inner: inner, ws: ws, report: report}
}
func (c *checked) Name() string            { return c.inner.Name() }
func (c *checked) Description() string     { return c.inner.Description() }
func (c *checked) Schema() json.RawMessage { return c.inner.Schema() }
func (c *checked) Mode() agent.ToolMode    { return c.inner.Mode() }
func (c *checked) Run(ctx context.Context, args json.RawMessage) (string, error) {
	out, err := c.inner.Run(ctx, args)
	if err != nil {
		return out, err
	}
	var in struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(args, &in) == nil && in.Path != "" {
		c.report.Check(c.ws, in.Path)
	}
	return out, err
}
