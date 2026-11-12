package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	imagecontract "github.com/keshon/gektor/internal/capabilities/images/contract"
	"github.com/keshon/gektor/internal/capabilities/media"
	capnodes "github.com/keshon/gektor/internal/capabilities/nodes"
	"github.com/keshon/gektor/internal/capabilities/social"
	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/executables"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/credentials"
	artifactnodes "github.com/keshon/gektor/internal/workflow/nodes/artifact"
	httpnode "github.com/keshon/gektor/internal/workflow/nodes/http"
	modelnode "github.com/keshon/gektor/internal/workflow/nodes/model"
	processnode "github.com/keshon/gektor/internal/workflow/nodes/process"
	"github.com/keshon/gektor/internal/workflow/nodes/record"
	scriptnode "github.com/keshon/gektor/internal/workflow/nodes/script"
	workflownodes "github.com/keshon/gektor/internal/workflow/nodes/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workspace"
)

// WorkflowRegistry composes operations without constructing a model client.
func WorkflowRegistry(ws *workspace.Workspace, store string, output imagecontract.OutputAuthorizer, fileOutput func(context.Context, string, string, json.RawMessage) error) (*workflow.Registry, error) {
	socialReader := social.New()
	applicationPath := userdirs.Sub("applications.json")
	applications := executables.New(applicationPath)
	mediaService := media.New(ws, applications)
	credentialStore := credentials.Store{Directory: userdirs.Sub("credentials")}
	model := workflowModelNode(ws)

	nodes := capnodes.ImageDefinitions(ws, output)
	nodes = append(nodes, capnodes.SocialDefinitions(socialReader)...)
	nodes = append(nodes, capnodes.FileDefinitions(ws, fileOutput)...)
	nodes = append(nodes, capnodes.MediaDefinitions(mediaService)...)
	nodes = append(nodes, capnodes.DocumentDefinitions(ws)...)
	sshService := ssh.New(ssh.Config{Workspace: ws, ProfilesPath: userdirs.Sub("ssh.json")})
	var authorizeSSHFile ssh.FileAuthorizer
	if fileOutput != nil {
		authorizeSSHFile = func(ctx context.Context, action, path string) error {
			args, err := json.Marshal(struct {
				Path string `json:"path"`
			}{Path: path})
			if err != nil {
				return err
			}
			return fileOutput(ctx, action, path, args)
		}
	}
	nodes = append(nodes, capnodes.SSHDefinitions(sshService, authorizeSSHFile)...)
	nodes = append(nodes, record.Definitions(ws, store)...)
	nodes = append(nodes, artifactnodes.Definitions(ws)...)
	nodes = append(nodes, processnode.Definition(ws))
	nodes = append(nodes, workflownodes.Definitions()...)
	nodes = append(nodes, scriptnode.Definition(), model, httpnode.Definition(credentialStore.Resolve))
	return workflow.NewRegistry(nodes...)
}

func workflowModelNode(ws *workspace.Workspace) workflow.Node {
	model := modelnode.Definition(func(ctx context.Context, path string) (modelnode.Connection, error) {
		return loadWorkflowModel(ctx, ws, path)
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
			rel, resolveErr := ws.Relative(path)
			if resolveErr != nil {
				return nil, resolveErr
			}
			paths[i] = filepath.ToSlash(rel)
		}
		return paths, nil
	}

	return model
}

// Workflow model profiles have a tighter limit than standalone profile imports.
const maxWorkflowProfileBytes = 64 << 10

func loadWorkflowModel(ctx context.Context, ws *workspace.Workspace, path string) (modelnode.Connection, error) {
	if ws == nil {
		return modelnode.Connection{}, fmt.Errorf("workspace unavailable")
	}

	file, err := ws.Open(path)
	if err != nil {
		return modelnode.Connection{}, err
	}
	// Closing a read-only profile cannot change the decoded settings.
	defer file.Close()

	limited := io.LimitReader(file, maxWorkflowProfileBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return modelnode.Connection{}, err
	}
	if len(data) > maxWorkflowProfileBytes {
		return modelnode.Connection{}, fmt.Errorf("profile too large")
	}

	reader := bytes.NewReader(data)
	profile, err := config.Decode(reader)
	if err != nil {
		return modelnode.Connection{}, err
	}

	options := BackendOptions{
		Kind:             profile.Provider.Kind,
		URL:              profile.Provider.URL,
		Model:            profile.Provider.Model,
		APIKeyEnv:        profile.Provider.APIKeyEnv,
		ReasoningEffort:  profile.Generation.ReasoningEffort,
		ReasoningSummary: profile.Generation.ReasoningSummary,
	}
	client, err := NewBackend(ctx, options)

	return modelnode.Connection{
		Client:       client,
		Name:         profile.Provider.Kind + "/" + profile.Provider.Model,
		MaxTokens:    profile.Generation.MaxTokens,
		ContextLimit: profile.Generation.ContextLimit,
		Temperature:  profile.Generation.Temperature,
	}, err
}

// NewWorkflow composes the headless application service and its policy.
func NewWorkflow(ws *workspace.Workspace, directory string, policy permission.Policy, gate permission.Gate) (service.Service, error) {
	if directory == "" {
		directory = userdirs.Sub("workflows")
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

	registry, err := WorkflowRegistry(ws, directory, output, resource)
	read := func(ctx context.Context, name string, args json.RawMessage) error {
		if name != "read_file" {
			return fmt.Errorf("unsupported service access %s", name)
		}
		var in struct{ Path string }
		decodeErr := json.Unmarshal(args, &in)
		if decodeErr != nil {
			return decodeErr
		}

		rel, resolveErr := ws.Relative(in.Path)
		if resolveErr != nil {
			return resolveErr
		}
		return resource(ctx, name, filepath.ToSlash(rel), args)
	}

	return service.Service{
		WS:                ws,
		StoreDir:          directory,
		Registry:          registry,
		Authorize:         read,
		AuthorizeResource: resource,
	}, err
}
