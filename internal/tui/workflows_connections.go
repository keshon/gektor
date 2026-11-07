package tui

import (
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"sort"
	"strconv"
	"strings"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/tui/components/schemaform"
	editor "github.com/keshon/gektor/internal/tui/workflow"
)

type workflowConnectionEdit struct {
	From, To, Field, Path, Draft string
	Pointer                      string
	FixedSource                  bool
	Old                          *editor.Connection
	Remove                       bool
}

func (m *model) queueConnection(edit workflowConnectionEdit) {
	b, _ := json.Marshal(edit)
	m.queueWorkflow("connection-edit", string(b))
}

func (m *model) workflowConnections() {
	w := m.workflows
	links := w.editor.Connections(w.graph.Selected)
	if len(links) == 0 {
		w.setNotice(pane.NoticeInfo, "No connections for this node · C creates one")
		return
	}
	choices := make([]string, len(links))
	for i, c := range links {
		choices[i] = strconv.Itoa(i+1) + ". " + c.Label()
	}
	draft := w.editor.Source()
	m.openForm("Connections / "+w.graph.Selected, "Incoming and outgoing connections. Choose one to edit or remove.", "Next",
		[]formField{{Key: "connection", Label: "Connection", Value: choices[0]}, {Key: "action", Label: "Action", Value: "Edit"}}, func(v map[string]string) error {
			if w.editor.Source() != draft {
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
		})
	m.dialog.form.Choices = map[string][]string{"connection": choices, "action": {"Edit", "Remove"}}
}

func (m *model) workflowConnectionTarget(from, to string, old *editor.Connection) {
	w := m.workflows
	if w.run != nil || w.busy {
		return
	}
	var dest automation.Step
	var names []string
	for _, s := range w.editor.Definition.Steps {
		names = append(names, s.ID)
		if s.ID == to {
			dest = s
		}
	}
	if dest.ID == "" {
		return
	}
	node, err := w.registry.Node(dest.Tool)
	if err != nil {
		w.setNotice(pane.NoticeError, err.Error())
		return
	}
	paths := map[string]editor.Connection{"Run after (no data)": {To: to, Field: "depends_on"}}
	var arguments []string
	for name := range schemaform.Parse(node.InputSchema).Properties {
		label := "Argument: " + name
		arguments = append(arguments, label)
		paths[label] = editor.Connection{To: to, Field: "args", Path: "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(name)}
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
	draft := w.editor.Source()
	m.openForm("Connect to "+to, "Choose the source and destination value. The next screen shows the current value.", "Next",
		[]formField{{Key: "from", Label: "Source node", Value: from}, {Key: "arg", Label: "Destination", Value: selected}}, func(v map[string]string) error {
			if w.editor.Source() != draft {
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
		})
	m.dialog.form.Choices = map[string][]string{"from": names, "arg": options}
}

func connectionValue(d automation.Definition, to, field, path string) string {
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

func (m *model) workflowConnectionEdit(edit workflowConnectionEdit) {
	w := m.workflows
	if w.busy || w.run != nil {
		return
	}
	if edit.Draft != w.editor.Source() {
		w.setNotice(pane.NoticeWarning, "Draft changed · reopen connections")
		return
	}
	if edit.Remove {
		if edit.Old == nil {
			return
		}
		c := *edit.Old
		current := connectionValue(w.editor.Definition, c.To, c.Field, c.Path)
		m.openRemoval("Remove connection", c.Label()+". Required values remain missing until repaired.", "Remove", []formField{{Key: "current", Label: "Current destination value", Value: cleanText(current), ReadOnly: true, Multiline: true}}, func(map[string]string) error {
			if w.editor.Source() != edit.Draft {
				return fmt.Errorf("draft changed; reopen connections")
			}
			if err := w.editor.ChangeConnection(&c, nil); err != nil {
				return err
			}
			m.workflowConnectionChanged()
			return nil
		})
		return
	}
	if edit.Field == "" {
		m.workflowConnectionTarget(edit.From, edit.To, edit.Old)
		return
	}
	c := editor.Connection{From: edit.From, To: edit.To, Field: edit.Field, Path: edit.Path}
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
		for _, s := range w.editor.Definition.Steps {
			if s.ID == c.From {
				if node, err := w.registry.Node(s.Tool); err == nil {
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
	current := connectionValue(w.editor.Definition, c.To, c.Field, c.Path)
	fields = append([]formField{{Key: "current", Label: "Current destination: " + c.Field + c.Path, Value: cleanText(current), ReadOnly: true, Multiline: true}}, fields...)
	m.openForm("Map "+c.From+" → "+c.To, "Schema fields are hints. Manual pointers support dynamic outputs.", "Apply", fields, func(v map[string]string) error {
		if w.editor.Source() != edit.Draft {
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
		if err := w.editor.ChangeConnection(edit.Old, &c); err != nil {
			return err
		}
		m.workflowConnectionChanged()
		return nil
	})
	m.dialog.form.Choices = choices
}

func portLabelForDisplay(pointer string) string {
	if pointer == "" {
		return "whole output"
	}
	return strings.TrimPrefix(pointer, "/")
}

func (m *model) workflowConnectionChanged() {
	w := m.workflows
	w.graph = m.workflowGraph(w.graph)
	w.setNotice(pane.NoticeSuccess, "Connection changed · V validates the draft")
	// Required-argument errors are visible immediately, without running a node.
	for _, s := range w.editor.Definition.Steps {
		node, err := w.registry.Node(s.Tool)
		if err != nil {
			continue
		}
		if err := automation.ValidateJSON(s.Args, automation.TemplateSchema(node.InputSchema)); err != nil {
			w.setNotice(pane.NoticeError, s.ID+": "+err.Error())
			w.graph.Selected = s.ID
			return
		}
	}
}
