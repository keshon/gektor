// Package tools holds concrete agent.Tool implementations: filesystem
// access, shell execution, and delegation to subagents. Each tool is a
// small, self-contained type — no shared "dispatcher" indirection.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/keshon/gektor/internal/agent"
	imagecontract "github.com/keshon/gektor/internal/capabilities/images/contract"
	"github.com/keshon/gektor/internal/capabilities/media"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/permission"
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

		catalog.Native = append(catalog.Native, &NativePackage{
			ID:           p.id,
			Description:  p.description,
			Instructions: nativeInstructions(p.id),
			Version:      "1",
			Exposure:     exposure,
			Tools:        list,
			ReadOnly:     readOnly,
		})
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

// Base returns the standard tool set shared by the main agent and
// subagents. Pass extra tools (ask_user, delegate_task, read_history) for the main
// agent only. File-mutating tools run post-write deterministic checks
// through the supplied reporter; a nil reporter builds unwrapped.
// vision enables reading pictures through read_file; it must come from a
// backend capability probe, not a guess.
func Base(ws *workspace.Workspace, procs *BackgroundProcesses, report *checks.Reporter, vision bool, extra ...agent.Tool) *agent.Registry {
	return agent.NewRegistry(nativeTools(Dependencies{
		WS:     ws,
		Procs:  procs,
		Checks: report,
		Vision: vision,
		Extra:  extra,
	}, false)...)
}

// ReadOnly returns a tool set that can inspect the workspace and running
// processes but cannot change anything — no writes, no shell, no process
// control. For workers whose job is to look and report (codebase mapping,
// final review): removing the mutating tools entirely beats prompting a
// weak model not to use them.
func ReadOnly(ws *workspace.Workspace, procs *BackgroundProcesses, vision bool) *agent.Registry {
	return agent.NewRegistry(nativeTools(Dependencies{
		WS:     ws,
		Procs:  procs,
		Vision: vision,
	}, true)...)
}
