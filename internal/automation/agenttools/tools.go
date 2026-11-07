// Package agenttools adapts the automation service to agent tool calls.
package agenttools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/automation/service"
	"github.com/keshon/gektor/internal/integrations/pixelita"
	"github.com/keshon/gektor/internal/workspace"
	"io"
)

type Tool struct {
	WS                *workspace.Workspace
	StoreDir          string
	ValidateOnly      bool
	Authorize         func(context.Context, string, json.RawMessage) error
	AuthorizeResource func(context.Context, string, string, json.RawMessage) error
	AuthorizeOutput   pixelita.OutputAuthorizer
}

func (t Tool) service() (service.Service, error) {
	registry, err := app.AutomationRegistry(t.WS, t.StoreDir, t.AuthorizeOutput, t.AuthorizeResource)
	authorize := t.AuthorizeResource
	return service.Service{WS: t.WS, StoreDir: t.StoreDir, ValidateOnly: t.ValidateOnly, Registry: registry, Authorize: t.Authorize, AuthorizeResource: authorize}, err
}
func (t Tool) Name() string {
	if t.ValidateOnly {
		return "workflow_validate"
	}
	return "workflow_run"
}
func (t Tool) ReadOnly() bool { return t.ValidateOnly }
func (t Tool) Mode() agent.ToolMode {
	if t.ValidateOnly {
		return agent.Concurrent
	}
	return agent.Exclusive
}
func (t Tool) Description() string {
	return (service.Service{ValidateOnly: t.ValidateOnly}).Description()
}
func (t Tool) Schema() json.RawMessage { return (service.Service{}).Schema() }
func (t Tool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	s, err := t.service()
	if err != nil {
		return "", err
	}
	return s.Run(ctx, args)
}

type definitionTool struct{ Tool }

func (t definitionTool) Schema() json.RawMessage {
	s, err := t.service()
	if err != nil {
		return json.RawMessage(`false`)
	}
	return s.DefinitionSchema()
}
func Resources(args json.RawMessage) ([]string, error) { return service.Resources(args) }
func decode(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("expected one JSON object")
	}
	return nil
}
