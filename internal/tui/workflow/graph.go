package workflow

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/keshon/gektor/internal/automation/recipe"
	"github.com/keshon/gektor/internal/tui/components/graph"
)

func inputPort(c Connection) string {
	if c.Field == "depends_on" {
		return "@order"
	}
	return c.Field + c.Path
}
func portLabel(key string) string {
	switch key {
	case "@order":
		return "after"
	case "for_each":
		return "each item"
	case "when":
		return "condition"
	}
	key = strings.TrimPrefix(key, "args/")
	key = strings.TrimPrefix(key, "/")
	key = strings.NewReplacer("~1", "/", "~0", "~").Replace(key)
	return strings.ReplaceAll(key, "/", ".")
}

func operationLabel(tool string) string {
	switch tool {
	case "docx_read":
		return "Read DOCX"
	case "xlsx_read":
		return "Read XLSX"
	case "pdf_read":
		return "Read PDF"
	case "script_transform":
		return "JavaScript"
	case "artifact_write":
		return "Save artifact"
	case "artifact_read_text":
		return "Read artifact"
	case "run_program":
		return "Run program"
	case "workflow_call":
		return "Workflow"
	}
	label := strings.ReplaceAll(tool, "_", " ")
	if label != "" {
		label = strings.ToUpper(label[:1]) + label[1:]
	}
	return label
}

func (e *Editor) Graph(old graph.Model) graph.Model {
	g := graph.Model{Selected: old.Selected, Pan: old.Pan}
	links := e.Connections("")
	for _, step := range e.Definition.Steps {
		inputs, outputs := map[string]string{"@order": "after"}, map[string]string{"": "result", "@order": "then"}
		var walk func(json.RawMessage, string, int)
		walk = func(raw json.RawMessage, path string, depth int) {
			var object map[string]json.RawMessage
			if depth < 2 && json.Unmarshal(raw, &object) == nil && len(object) > 0 && object["$step"] == nil && object["$input"] == nil {
				keys := make([]string, 0, len(object))
				for key := range object {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					if path == "" && key == "code" {
						continue
					}
					walk(object[key], path+"/"+escapePointer(key), depth+1)
				}
				return
			}
			if path != "" {
				inputs["args"+path] = portLabel("args" + path)
			}
		}
		walk(step.Args, "", 0)
		if len(step.ForEach) > 0 {
			inputs["for_each"] = "each item"
		}
		if len(step.When) > 0 {
			inputs["when"] = "condition"
		}
		for _, c := range links {
			if c.To == step.ID {
				inputs[inputPort(c)] = portLabel(inputPort(c))
			}
			if c.From == step.ID {
				key, label := c.Pointer, portLabel(c.Pointer)
				if c.Field == "depends_on" {
					key, label = "@order", "then"
				} else if key == "" {
					label = "result"
				}
				outputs[key] = label
			}
		}
		// Outputs exported by the recipe are useful sockets even with no downstream node.
		var exported func(json.RawMessage)
		exported = func(raw json.RawMessage) {
			var object map[string]json.RawMessage
			if json.Unmarshal(raw, &object) == nil && object != nil {
				var id, pointer string
				_ = json.Unmarshal(object["$step"], &id)
				_ = json.Unmarshal(object["pointer"], &pointer)
				if id == step.ID && pointer != "" {
					outputs[pointer] = portLabel(pointer)
				}
				for _, value := range object {
					exported(value)
				}
				return
			}
			var array []json.RawMessage
			if json.Unmarshal(raw, &array) == nil {
				for _, value := range array {
					exported(value)
				}
			}
		}
		for _, value := range e.Definition.Outputs {
			exported(value)
		}
		ports := func(values map[string]string) []graph.Port {
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Slice(keys, func(i, j int) bool {
				if keys[i] == "@order" {
					return false
				}
				if keys[j] == "@order" {
					return true
				}
				return keys[i] < keys[j]
			})
			out := make([]graph.Port, 0, len(keys))
			for _, key := range keys {
				out = append(out, graph.Port{Key: key, Label: values[key]})
			}
			return out
		}
		detail := ""
		if step.Review {
			detail = "Review before running"
		} else if string(step.When) == "false" {
			detail = "Disabled / condition false"
		} else if len(step.ForEach) > 0 {
			detail = "For each item"
		}
		g.Nodes = append(g.Nodes, graph.Node{ID: step.ID, Label: operationLabel(step.Tool), Detail: detail, Inputs: ports(inputs), Outputs: ports(outputs)})
	}
	for _, c := range links {
		fromPort := c.Pointer
		if c.Field == "depends_on" {
			fromPort = "@order"
		}
		g.Edges = append(g.Edges, graph.Edge{From: c.From, To: c.To, FromPort: fromPort, ToPort: inputPort(c), Data: c.Field != "depends_on"})
	}
	positions := map[string]graph.Point{}
	for _, n := range old.Nodes {
		positions[n.ID] = n.Point
	}
	defaults := map[string]graph.Point{}
	if ordered, err := recipe.Order(e.Definition.Steps); err == nil {
		depth, rows := map[string]int{}, map[int]int{}
		for _, step := range ordered {
			level := 0
			for _, id := range step.DependsOn {
				level = max(level, depth[id]+1)
			}
			depth[step.ID] = level
			defaults[step.ID] = graph.Point{X: 2 + level*(graph.NodeWidth+10), Y: 1 + rows[level]}
			for _, n := range g.Nodes {
				if n.ID == step.ID {
					rows[level] += n.Height() + 3
					break
				}
			}
		}
	}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if p, ok := positions[n.ID]; ok {
			n.Point = p
		} else if p, ok := defaults[n.ID]; ok {
			n.Point = p
		} else {
			n.Point = graph.Point{X: 2 + (i%3)*(graph.NodeWidth+10), Y: 1 + (i/3)*14}
		}
	}
	if g.Selected == "" && len(g.Nodes) > 0 {
		g.Selected = g.Nodes[0].ID
	}
	return g
}
