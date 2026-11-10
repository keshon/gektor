package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/schemaform"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/workflow"
)

type workflowConnectionEdit struct {
	From, To, Field, Path, Draft string
	Pointer                      string
	FixedSource                  bool
	Old                          *Connection
	Remove                       bool
}

func (m *Model) queueConnection(edit workflowConnectionEdit) {
	b, _ := json.Marshal(edit)
	m.queueAction("connection-edit", string(b))
}

func (m *Model) connections() {
	links := m.editor.Connections(m.graph.Selected)
	if len(links) == 0 {
		m.setNotice(pane.NoticeInfo, "No connections for this node · C creates one")
		return
	}
	choices := make([]string, len(links))
	for i, c := range links {
		choices[i] = strconv.Itoa(i+1) + ". " + c.Label()
	}
	draft := m.editor.Source()
	m.dialogs.OpenForm(screen.Form{
		Title:      "Connections / " + m.graph.Selected,
		Hint:       "Incoming and outgoing connections. Choose one to edit or remove.",
		ApplyLabel: "Next",
		Fields:     []formField{{Key: "connection", Label: "Connection", Value: choices[0]}, {Key: "action", Label: "Action", Value: "Edit"}},
		Apply: func(v map[string]string) error {
			if m.editor.Source() != draft {
				return fmt.Errorf("draft changed; reopen connections")
			}
			for i, label := range choices {
				if v["connection"] != label {
					continue
				}
				c := links[i]
				if v["action"] != "Edit" && v["action"] != "Remove" {
					return fmt.Errorf("choose Edit or Remove")
				}
				m.queueConnection(workflowConnectionEdit{From: c.From, To: c.To, Old: &c, Remove: v["action"] == "Remove", Draft: draft})
				return nil
			}
			return fmt.Errorf("choose a connection")
		},
		Choices: map[string][]string{"connection": choices, "action": {"Edit", "Remove"}},
	})

}

func (m *Model) connectionTarget(from, to string, old *Connection) {
	if m.run != nil || m.busy {
		return
	}
	var dest workflow.Step
	var names []string
	for _, s := range m.editor.Definition.Steps {
		names = append(names, s.ID)
		if s.ID == to {
			dest = s
		}
	}
	if dest.ID == "" {
		return
	}
	node, err := m.registry.Node(dest.Tool)
	if err != nil {
		m.setNotice(pane.NoticeError, err.Error())
		return
	}
	paths := map[string]Connection{"Run after (no data)": {To: to, Field: "depends_on"}}
	var arguments []string
	for name := range schemaform.Parse(node.InputSchema).Properties {
		label := "Argument: " + name
		arguments = append(arguments, label)
		paths[label] = Connection{To: to, Field: "args", Path: "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(name)}
	}
	sort.Strings(arguments)
	options := append([]string{"Run after (no data)"}, arguments...)
	selected := options[0]
	if old != nil {
		for label, c := range paths {
			if c.Field == old.Field && c.Path == old.Path {
				selected = label
				break
			}
		}
		if old.Field != "depends_on" && selected == options[0] {
			selected = "Current: " + old.Field + old.Path
			paths[selected] = *old
			options = append(options, selected)
		}
	}
	draft := m.editor.Source()
	m.dialogs.OpenForm(screen.Form{
		Title:      "Connect to " + to,
		Hint:       "Choose the source and destination value. The next screen shows the current value.",
		ApplyLabel: "Next",
		Fields:     []formField{{Key: "from", Label: "Source node", Value: from}, {Key: "arg", Label: "Destination", Value: selected}},
		Apply: func(v map[string]string) error {
			if m.editor.Source() != draft {
				return fmt.Errorf("draft changed; reopen connections")
			}
			c, ok := paths[v["arg"]]
			if !ok {
				return fmt.Errorf("choose a destination argument")
			}
			found := false
			for _, name := range names {
				if name == v["from"] {
					found = true
				}
			}
			if !found || v["from"] == to {
				return fmt.Errorf("choose a different source node")
			}
			m.queueConnection(workflowConnectionEdit{From: v["from"], To: to, Field: c.Field, Path: c.Path, Old: old, Draft: draft})
			return nil
		},
		Choices: map[string][]string{"from": names, "arg": options},
	})

}

func connectionValue(d workflow.Definition, to, field, path string) string {
	for _, s := range d.Steps {
		if s.ID != to {
			continue
		}
		var raw json.RawMessage
		switch field {
		case "args":
			raw = s.Args
		case "when":
			raw = s.When
		case "for_each":
			raw = s.ForEach
		case "depends_on":
			b, _ := json.Marshal(s.DependsOn)
			return string(b)
		}
		if path != "" {
			for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
				key := strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
				var obj map[string]json.RawMessage
				if json.Unmarshal(raw, &obj) == nil && obj != nil {
					raw = obj[key]
					continue
				}
				var array []json.RawMessage
				index, err := strconv.Atoi(key)
				if json.Unmarshal(raw, &array) != nil || err != nil || index < 0 || index >= len(array) {
					return "(unset)"
				}
				raw = array[index]
			}
		}
		if len(raw) == 0 {
			return "(unset)"
		}
		return string(raw)
	}
	return "(missing node)"
}

// Output schemas offer hints only. Dynamic properties and array indices still
// use manual JSON pointers; schema fields do not prove a runtime value exists.
func workflowOutputPointers(raw json.RawMessage) []string {
	var out []string
	var walk func(json.RawMessage, string, int)
	walk = func(raw json.RawMessage, path string, depth int) {
		if depth > 8 || len(out) >= 128 {
			return
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(raw, &schema) != nil {
			return
		}
		keys := make([]string, 0, len(schema.Properties))
		for key := range schema.Properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if len(out) >= 128 {
				return
			}
			p := path + "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(key)
			out = append(out, p)
			walk(schema.Properties[key], p, depth+1)
		}
	}
	walk(raw, "", 0)
	return out
}

func (m *Model) connectionEdit(edit workflowConnectionEdit) {
	if m.busy || m.run != nil {
		return
	}
	if edit.Draft != m.editor.Source() {
		m.setNotice(pane.NoticeWarning, "Draft changed · reopen connections")
		return
	}
	if edit.Remove {
		if edit.Old == nil {
			return
		}
		c := *edit.Old
		current := connectionValue(m.editor.Definition, c.To, c.Field, c.Path)
		m.dialogs.OpenForm(screen.Form{
			Title:      "Remove connection",
			Hint:       c.Label() + ". Required values remain missing until repaired.",
			ApplyLabel: "Remove",
			Fields:     []formField{{Key: "current", Label: "Current destination value", Value: cleanText(current), ReadOnly: true, Multiline: true}},
			Apply: func(map[string]string) error {
				if m.editor.Source() != edit.Draft {
					return fmt.Errorf("draft changed; reopen connections")
				}
				if err := m.editor.ChangeConnection(&c, nil); err != nil {
					return err
				}
				m.connectionChanged()
				return nil
			},
			Destructive:   true,
			StartOnCancel: true,
		})
		return
	}
	if edit.Field == "" {
		m.connectionTarget(edit.From, edit.To, edit.Old)
		return
	}
	c := Connection{From: edit.From, To: edit.To, Field: edit.Field, Path: edit.Path}
	pointer := edit.Pointer
	if edit.Old != nil {
		pointer = edit.Old.Pointer
	}
	fields := []formField{}
	choices := map[string][]string{}
	if edit.FixedSource {
		fields = append(fields, formField{Key: "source", Label: "Source", Value: c.From + " / " + portLabelForDisplay(pointer), ReadOnly: true})
	} else if c.Field != "depends_on" {
		var hints []string
		for _, s := range m.editor.Definition.Steps {
			if s.ID == c.From {
				if node, err := m.registry.Node(s.Tool); err == nil {
					hints = workflowOutputPointers(node.OutputSchema)
				}
			}
		}
		if len(hints) > 0 {
			fields = append(fields, formField{Key: "source_field", Label: "Source field (schema hint)", Value: "Manual JSON pointer"})
			choices["source_field"] = append([]string{"Manual JSON pointer", "Whole output"}, hints...)
		}
		fields = append(fields, formField{Key: "pointer", Label: "Manual JSON pointer (empty = whole output)", Value: pointer})
	}
	current := connectionValue(m.editor.Definition, c.To, c.Field, c.Path)
	fields = append([]formField{{Key: "current", Label: "Current destination: " + c.Field + c.Path, Value: cleanText(current), ReadOnly: true, Multiline: true}}, fields...)
	m.dialogs.OpenForm(screen.Form{
		Title:      "Map " + c.From + " → " + c.To,
		Hint:       "Schema fields are hints. Manual pointers support dynamic outputs.",
		ApplyLabel: "Apply",
		Fields:     fields,
		Apply: func(v map[string]string) error {
			if m.editor.Source() != edit.Draft {
				return fmt.Errorf("draft changed; reopen connections")
			}
			c.Pointer = v["pointer"]
			if edit.FixedSource {
				c.Pointer = edit.Pointer
			}
			if hint := v["source_field"]; hint != "" && hint != "Manual JSON pointer" {
				valid := false
				for _, option := range choices["source_field"] {
					if option == hint {
						valid = true
					}
				}
				if !valid {
					return fmt.Errorf("choose a source field")
				}
				if hint == "Whole output" {
					c.Pointer = ""
				} else {
					c.Pointer = hint
				}
			}
			if err := m.editor.ChangeConnection(edit.Old, &c); err != nil {
				return err
			}
			m.connectionChanged()
			return nil
		},
		Choices: choices,
	})

}

func portLabelForDisplay(pointer string) string {
	if pointer == "" {
		return "whole output"
	}
	return strings.TrimPrefix(pointer, "/")
}

func (m *Model) connectionChanged() {
	m.graph = m.buildGraph(m.graph)
	m.setNotice(pane.NoticeSuccess, "Connection changed · V validates the draft")
	// Required-argument errors are visible immediately, without running a node.
	for _, s := range m.editor.Definition.Steps {
		node, err := m.registry.Node(s.Tool)
		if err != nil {
			continue
		}
		if err := workflow.ValidateJSON(s.Args, workflow.TemplateSchema(node.InputSchema)); err != nil {
			m.setNotice(pane.NoticeError, s.ID+": "+err.Error())
			m.graph.Selected = s.ID
			return
		}
	}
}
