package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/credentials"
	artifactnodes "github.com/keshon/gektor/internal/automation/nodes/artifact"
	httpnode "github.com/keshon/gektor/internal/automation/nodes/http"
	modelnode "github.com/keshon/gektor/internal/automation/nodes/model"
	processnode "github.com/keshon/gektor/internal/automation/nodes/process"
	"github.com/keshon/gektor/internal/automation/nodes/record"
	scriptnode "github.com/keshon/gektor/internal/automation/nodes/script"
	workflownodes "github.com/keshon/gektor/internal/automation/nodes/workflow"
	"github.com/keshon/gektor/internal/automation/service"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/gektor"
	documentnodes "github.com/keshon/gektor/internal/integrations/documents/nodes"
	filenodes "github.com/keshon/gektor/internal/integrations/fileops/nodes"
	"github.com/keshon/gektor/internal/integrations/pixelita"
	imagenodes "github.com/keshon/gektor/internal/integrations/pixelita/nodes"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workspace"
	"io"
	"path/filepath"
)

// AutomationRegistry composes operations without constructing a model client.
func AutomationRegistry(ws *workspace.Workspace, store string, output pixelita.OutputAuthorizer, fileOutput func(context.Context, string, string, json.RawMessage) error) (*automation.Registry, error) {
	nodes := imagenodes.Definitions(ws, output)
	nodes = append(nodes, filenodes.Definitions(ws, fileOutput)...)
	nodes = append(nodes, documentnodes.Definitions(ws)...)
	nodes = append(nodes, record.Definitions(ws, store)...)
	nodes = append(nodes, artifactnodes.Definitions(ws)...)
	nodes = append(nodes, processnode.Definition(ws))
	nodes = append(nodes, workflownodes.Definitions()...)
	model := modelnode.Definition(func(ctx context.Context, path string) (modelnode.Connection, error) {
		if ws == nil {
			return modelnode.Connection{}, fmt.Errorf("workspace unavailable")
		}
		f, err := ws.Open(path)
		if err != nil {
			return modelnode.Connection{}, err
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
		if err != nil {
			return modelnode.Connection{}, err
		}
		if len(data) > 64<<10 {
			return modelnode.Connection{}, fmt.Errorf("profile too large")
		}
		var profile config.Profile
		if err := json.Unmarshal(data, &profile); err != nil {
			return modelnode.Connection{}, err
		}
		if err := profile.Validate(); err != nil {
			return modelnode.Connection{}, err
		}
		client, err := NewBackend(ctx, BackendOptions{Kind: profile.Provider.Kind, URL: profile.Provider.URL, Model: profile.Provider.Model, APIKeyEnv: profile.Provider.APIKeyEnv, ReasoningEffort: profile.Generation.ReasoningEffort, ReasoningSummary: profile.Generation.ReasoningSummary})
		return modelnode.Connection{Client: client, Name: profile.Provider.Kind + "/" + profile.Provider.Model, MaxTokens: profile.Generation.MaxTokens, ContextLimit: profile.Generation.ContextLimit, Temperature: profile.Generation.Temperature}, err
	})
	resources := model.Resources
	model.Resources = func(args json.RawMessage) ([]string, error) {
		paths, err := resources(args)
		if err != nil {
			return nil, err
		}
		for i, path := range paths {
			if ws == nil {
				return nil, fmt.Errorf("workspace unavailable")
			}
			rel, err := ws.Relative(path)
			if err != nil {
				return nil, err
			}
			paths[i] = filepath.ToSlash(rel)
		}
		return paths, nil
	}
	nodes = append(nodes, scriptnode.Definition(), model, httpnode.Definition((credentials.Store{Directory: gektor.Sub("credentials")}).Resolve))
	return automation.NewRegistry(nodes...)
}

// NewAutomation composes the headless application service and its policy.
func NewAutomation(ws *workspace.Workspace, directory string, policy permission.Policy, gate permission.Gate) (service.Service, error) {
	if directory == "" {
		directory = gektor.Sub("workflows")
	}
	output := func(ctx context.Context, path string, args json.RawMessage) error {
		rel, err := ws.Relative(path)
		if err != nil {
			return err
		}
		return permission.Authorize(ctx, policy, gate, "image_variants", filepath.ToSlash(rel), args)
	}
	resource := func(ctx context.Context, name, path string, args json.RawMessage) error {
		return permission.Authorize(ctx, policy, gate, name, path, args)
	}
	registry, err := AutomationRegistry(ws, directory, output, resource)
	read := func(ctx context.Context, name string, args json.RawMessage) error {
		if name != "read_file" {
			return fmt.Errorf("unsupported service access %s", name)
		}
		var in struct{ Path string }
		if err := json.Unmarshal(args, &in); err != nil {
			return err
		}
		rel, err := ws.Relative(in.Path)
		if err != nil {
			return err
		}
		return resource(ctx, name, filepath.ToSlash(rel), args)
	}
	return service.Service{WS: ws, StoreDir: directory, Registry: registry, Authorize: read, AuthorizeResource: resource}, err
}
