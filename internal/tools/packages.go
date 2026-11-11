package tools

import (
	"context"
	"encoding/json"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/capabilities/media"
	"github.com/keshon/gektor/internal/executables"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow"
)

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
