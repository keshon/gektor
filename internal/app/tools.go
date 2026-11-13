package app

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/capabilities/social/contract"
	"github.com/keshon/gektor/internal/tools/extension"
)

// Toolset is display metadata, not an executable registry or activation policy.
type Toolset struct {
	ID, Name, Description, Group, Loading, Source, Connection, Status string
	Agent, Workflow                                                   []string
}

// Toolsets projects existing composition metadata without connecting optional adapters.
func Toolsets(packages []extension.PackageInfo, sources []*extension.Capability, overrides map[string]extension.Exposure, attached []agent.Tool) ([]Toolset, error) {
	nodes, err := WorkflowRegistry(nil, "", nil, nil)
	if err != nil {
		return nil, err
	}
	supported := map[string]bool{}
	for _, name := range nodes.Names() {
		supported[name] = true
	}
	sets := []Toolset{}
	for _, p := range packages {
		exposure := p.Exposure
		if value, ok := overrides[p.ID]; ok {
			exposure = value
		}
		label := strings.ReplaceAll(strings.TrimPrefix(p.ID, "native_"), "_", " ")
		if label != "" {
			label = string(unicode.ToUpper(rune(label[0]))) + label[1:]
		}
		if label == "Ssh" {
			label = "SSH"
		}
		set := Toolset{ID: p.ID, Name: label, Description: p.Description, Group: "Built-in", Loading: exposureLabel(exposure), Source: "Built-in", Agent: p.Tools}
		for _, name := range p.Tools {
			if supported[name] {
				set.Workflow = append(set.Workflow, name)
			}
		}
		sets = append(sets, set)
	}
	for _, source := range sources {
		if source == nil {
			continue
		}
		exposure := extension.Lazy
		if source.Eager {
			exposure = extension.Eager
		}
		name := source.ID
		if source.ID == "browser" || strings.HasPrefix(source.Source, "builtin:") {
			name = strings.ToUpper(name[:1]) + name[1:]
		}
		set := Toolset{ID: "source:" + source.ID, Name: name, Description: source.Description, Group: "External", Loading: exposureLabel(exposure), Source: source.Source, Agent: source.ToolNames()}
		state, _, detail := source.ConnectionStatus()
		set.Connection = state
		if detail != "" {
			set.Connection += " — " + detail
		}
		if strings.HasPrefix(source.Source, "builtin:") {
			set.Group = "Built-in"
		}
		for _, name := range set.Agent {
			node := name
			if name == contract.AgentName {
				node = "social_read_post"
			}
			if supported[node] {
				set.Workflow = append(set.Workflow, node)
			}
		}
		// The bundled social operation is known even before its adapter is loaded.
		if source.Source == "builtin:fxtwitter:1" {
			set.Agent = []string{contract.AgentName}
			set.Workflow = []string{"social_read_post"}
		}
		sets = append(sets, set)
	}
	if len(attached) > 0 {
		set := Toolset{ID: "launch_mcp", Name: "Launch MCP", Group: "External", Loading: "Always", Source: "-mcp launch argument", Description: "MCP tools connected for this launch. Connection management remains in the existing MCP view."}
		for _, tool := range attached {
			set.Agent = append(set.Agent, tool.Name())
		}
		sets = append(sets, set)
	}

	order := []string{"core", "workspace", "native_files", "native_documents", "native_images", "native_web", "source:browser", "native_media", "git", "execution", "processes", "native_ssh", "native_workflows"}
	ranks := make(map[string]int, len(order))
	for i, id := range order {
		ranks[id] = i
	}
	slices.SortStableFunc(sets, func(a, b Toolset) int {
		aRank, aKnown := ranks[a.ID]
		bRank, bKnown := ranks[b.ID]
		if !aKnown {
			aRank = len(order)
		}
		if !bKnown {
			bRank = len(order)
		}
		if aRank != bRank {
			return cmp.Compare(aRank, bRank)
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return sets, nil
}

func exposureLabel(value extension.Exposure) string {
	switch value {
	case extension.Always:
		return "Always"
	case extension.Eager:
		return "On load"
	default:
		return "On demand"
	}
}
