package tools

import (
	"fmt"
	"slices"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/checks"
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

type packageDependencies struct {
	WS     *workspace.Workspace
	Procs  *BackgroundProcesses
	Report FindingSink
	Rules  []checks.Rule
	Vision bool
	Extra  []agent.Tool
}

type toolEntry struct {
	build    func(packageDependencies) agent.Tool
	readOnly bool
}

type packageDefinition struct {
	id, description string
	exposure        Exposure
	entries         []toolEntry
}

func nativeTools(d packageDependencies, readOnly bool) []agent.Tool {
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
func Compose(ws *workspace.Workspace, procs *BackgroundProcesses, report FindingSink, rules []checks.Rule, vision bool, catalog *Capabilities, overrides map[string]Exposure, extra ...agent.Tool) (*agent.Registry, error) {
	d := packageDependencies{ws, procs, report, rules, vision, extra}
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
	all = append(all, extra...)
	for _, tool := range all {
		if slices.Contains(catalog.ForbiddenTools, tool.Name()) {
			return nil, fmt.Errorf("tool %q is unavailable to this role", tool.Name())
		}
	}
	if err := agent.NewRegistry(all...).Err(); err != nil {
		return nil, err
	}
	return agent.NewRegistry(append(baseline, extra...)...), nil
}
