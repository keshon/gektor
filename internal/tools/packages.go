package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	imagecontract "github.com/keshon/gektor/internal/capabilities/images/contract"
	"github.com/keshon/gektor/internal/capabilities/media"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/executables"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

// Exposure controls schemas, never execution permissions or transport lifetime.
type Exposure uint8

const (
	Always Exposure = iota
	Eager
	Lazy
)

const NativeWeb = "native_web"

// Dependencies are the services and policy required to build complete tools.
type Dependencies struct {
	StoreDir          string
	Policy            permission.Policy
	Gate              permission.Gate
	authorize         func(context.Context, string, json.RawMessage) error
	authorizeOutput   imagecontract.OutputAuthorizer
	authorizeResource func(context.Context, string, string, json.RawMessage) error
	checkSource       func(context.Context, string, json.RawMessage) error
	WS                *workspace.Workspace
	Procs             *BackgroundProcesses
	Checks            *checks.Reporter
	Vision            bool
	Extra             []agent.Tool
	Media             *media.Service
}

type toolEntry struct {
	build    func(Dependencies) agent.Tool
	readOnly bool
}

type packageDefinition struct {
	id, description string
	exposure        Exposure
	entries         []toolEntry
}

func nativeTools(d Dependencies, readOnly bool) []agent.Tool {
	d = d.prepared()
	var list []agent.Tool
	for _, p := range builtinPackages() {
		if p.exposure == Lazy {
			continue
		}
		for _, entry := range p.entries {
			if !readOnly || entry.readOnly {
				list = append(list, entry.build(d))
			}
		}
	}
	if readOnly {
		list = append(list, ReadHistory{})
	} else {
		list = append(list, d.Extra...)
	}
	return list
}

// Compose constructs one agent's native instances. Optional native packages
// are published during preparation, after saved activations have been restored.
// Overrides are internal experiment inputs; shipping callers leave them nil.
func Compose(d Dependencies, catalog *Capabilities, overrides map[string]Exposure) (*agent.Registry, error) {
	d = d.prepared()
	definitions := builtinPackages()
	for id, exposure := range overrides {
		optional := false
		for _, definition := range definitions {
			if definition.id == id && definition.exposure != Always {
				optional = true
				break
			}
		}
		if !optional || (exposure != Eager && exposure != Lazy) {
			return nil, fmt.Errorf("unsupported native exposure %q: %d", id, exposure)
		}
	}
	if err := catalog.validateSources(); err != nil {
		return nil, err
	}
	var baseline, all []agent.Tool
	for _, p := range definitions {
		var list []agent.Tool
		var readOnly []string
		for _, entry := range p.entries {
			t := entry.build(d)
			list = append(list, t)
			if entry.readOnly {
				readOnly = append(readOnly, t.Name())
			}
		}
		all = append(all, list...)
		if p.exposure == Always {
			baseline = append(baseline, list...)
			continue
		}
		exposure := p.exposure
		if value, ok := overrides[p.id]; ok {
			exposure = value
		}
		catalog.Native = append(catalog.Native, &NativePackage{ID: p.id, Description: p.description, Instructions: nativeInstructions(p.id), Version: "1", Exposure: exposure, Tools: list, ReadOnly: readOnly})
	}
	all = append(all, d.Extra...)
	for _, tool := range all {
		if slices.Contains(catalog.ForbiddenTools, tool.Name()) {
			return nil, fmt.Errorf("tool %q is unavailable to this role", tool.Name())
		}
	}
	if err := agent.NewRegistry(all...).Err(); err != nil {
		return nil, err
	}
	return agent.NewRegistry(append(baseline, d.Extra...)...), nil
}

func (d Dependencies) prepared() Dependencies {
	if d.StoreDir == "" {
		d.StoreDir = userdirs.Sub("workflows")
	}
	if len(d.Policy.Rules) == 0 {
		d.Policy = permission.Default()
	}
	d.authorize, d.authorizeOutput = workflowPolicy(d.WS, d.Policy, d.Gate)
	d.checkSource, _ = workflowPolicy(d.WS, d.Policy, nil)
	d.authorizeResource = func(ctx context.Context, name, resource string, args json.RawMessage) error {
		return permission.Authorize(ctx, d.Policy, d.Gate, name, resource, args)
	}
	if d.Media == nil {
		d.Media = media.New(d.WS, executables.New(userdirs.Sub("applications.json")))
	}
	return d
}

func (d Dependencies) registry() (*workflow.Registry, error) {
	return app.WorkflowRegistry(d.WS, d.StoreDir, d.authorizeOutput, d.authorizeResource)
}
