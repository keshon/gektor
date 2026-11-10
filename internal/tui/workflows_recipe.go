package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/schemaform"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
)

type workflowParameterEdit struct {
	Kind, Name, Draft string
	Remove            bool
}

const workflowInputSchema = `{"type":"object","properties":{"type":{"type":"string","enum":["string","integer","number","boolean","array","object"]},"required":{"type":"boolean","enum":[false,true]},"default":{}},"required":["type"],"additionalProperties":false}`

func (m *workflowScreen) recipeName() {
	w := m
	draft := w.editor.Source()
	m.dialogs.openForm(workflowForm{
		Title:      "Recipe name",
		Hint:       "This name identifies the recipe in saved runs.",
		ApplyLabel: "Apply",
		Fields:     []formField{{Key: "name", Label: "Name", Value: w.editor.Definition.Name}},
		Apply: func(v map[string]string) error {
			if strings.TrimSpace(v["name"]) == "" || len(v["name"]) > 120 {
				return fmt.Errorf("name must contain 1–120 bytes")
			}
			d := w.editor.Clone()
			d.Name = v["name"]
			return m.replaceRecipe(d, draft)
		},
	})
}

func (m *workflowScreen) replaceRecipe(d workflow.Definition, draft string) error {
	w := m
	if w.busy || w.run != nil {
		return fmt.Errorf("return to an idle editable draft first")
	}
	if w.editor.Source() != draft {
		return fmt.Errorf("draft changed; reopen recipe settings")
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	if len(b) > 64<<10 {
		return fmt.Errorf("recipe exceeds 64 KiB")
	}
	if string(b) != draft {
		w.editor.Replace(d)
		w.graph = m.buildGraph(w.graph)
	}
	w.setNotice(pane.NoticeSuccess, "Recipe updated · V validates the draft")
	return nil
}

func (m *workflowScreen) parameters(kind string) {
	w := m
	var names []string
	if kind == "inputs" {
		for name := range w.editor.Definition.Inputs {
			names = append(names, name)
		}
	} else {
		for name := range w.editor.Definition.Outputs {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	options := []string{"Add new"}
	mapping := map[string]string{}
	for _, name := range names {
		label := "Named: " + name
		options = append(options, label)
		mapping[label] = name
	}
	draft := w.editor.Source()
	m.dialogs.openForm(workflowForm{
		Title:      "Recipe / " + kind,
		Hint:       "Add, edit or remove one named value. Existing names stay fixed; references are not rewritten.",
		ApplyLabel: "Next",
		Fields:     []formField{{Key: "entry", Label: "Entry", Value: options[0]}, {Key: "action", Label: "Action", Value: "Edit"}},
		Apply: func(v map[string]string) error {
			if w.editor.Source() != draft {
				return fmt.Errorf("draft changed; reopen recipe settings")
			}
			name, exists := mapping[v["entry"]]
			if v["entry"] != "Add new" && !exists {
				return fmt.Errorf("choose an entry")
			}
			if v["action"] != "Edit" && v["action"] != "Remove" {
				return fmt.Errorf("choose Edit or Remove")
			}
			if !exists && v["action"] == "Remove" {
				return fmt.Errorf("choose an existing entry to remove")
			}
			edit := workflowParameterEdit{Kind: kind, Name: name, Draft: draft, Remove: v["action"] == "Remove"}
			b, _ := json.Marshal(edit)
			m.queueAction("parameter-edit", string(b))
			return nil
		},
		Choices: map[string][]string{"entry": options, "action": {"Edit", "Remove"}},
	})

}

func (m *workflowScreen) parameterEdit(edit workflowParameterEdit) {
	w := m
	if w.busy || w.run != nil {
		return
	}
	if edit.Kind != "inputs" && edit.Kind != "outputs" {
		return
	}
	if w.editor.Source() != edit.Draft {
		w.setNotice(pane.NoticeWarning, "Draft changed · reopen recipe settings")
		return
	}
	d := w.editor.Clone()
	if edit.Remove {
		var raw []byte
		if edit.Kind == "inputs" {
			raw, _ = json.Marshal(d.Inputs[edit.Name])
		} else {
			raw = d.Outputs[edit.Name]
		}
		m.dialogs.openForm(workflowForm{
			Title:      "Remove " + edit.Name,
			Hint:       "References to this name are not rewritten. Validate the recipe after removal.",
			ApplyLabel: "Remove",
			Fields:     []formField{{Key: "current", Label: "Current " + edit.Kind + " value", Value: cleanText(string(raw)), ReadOnly: true, Multiline: true}},
			Apply: func(map[string]string) error {
				if edit.Kind == "inputs" {
					delete(d.Inputs, edit.Name)
				} else {
					delete(d.Outputs, edit.Name)
				}
				return m.replaceRecipe(d, edit.Draft)
			},
			Destructive:   true,
			StartOnCancel: true,
		})
		return
	}
	fields := []formField{{Key: "name", Label: "Name", Value: edit.Name, ReadOnly: edit.Name != ""}}
	schema := schemaform.Parse(json.RawMessage(workflowInputSchema))
	var original json.RawMessage
	var choices map[string][]string
	if edit.Kind == "inputs" {
		choices = schema.Choices()
		input := workflow.Input{Type: "string"}
		if edit.Name != "" {
			input = d.Inputs[edit.Name]
		}
		original, _ = json.Marshal(input)
		fields = append(fields, schema.Fields(original)...)
	} else {
		value := d.Outputs[edit.Name]
		if len(value) == 0 {
			value = json.RawMessage(`null`)
		}
		fields = append(fields, formField{Key: "value", Label: "Value (JSON literal or reference)", Value: string(value), Multiline: true})
	}
	hint := "Default is a JSON value; leave it blank for no default. Required inputs can have defaults."
	if edit.Kind == "outputs" {
		hint = `Use a JSON value or reference, e.g. {"$step":"save","pointer":"/result"}. V validates referenced names.`
	}
	m.dialogs.openForm(workflowForm{
		Title:      "Recipe / " + edit.Kind,
		Hint:       hint,
		ApplyLabel: "Apply",
		Fields:     fields,
		Choices:    choices,
		Apply: func(v map[string]string) error {
			d := w.editor.Clone()
			name := v["name"]
			if edit.Name != "" {
				name = edit.Name
			}
			if strings.TrimSpace(name) == "" || len(name) > 64 {
				return fmt.Errorf("name must contain 1–64 bytes")
			}
			if edit.Kind == "inputs" {
				if _, exists := d.Inputs[name]; exists && edit.Name == "" {
					return fmt.Errorf("input name already exists")
				}
				raw, err := schema.Values(original, v)
				if err != nil {
					return err
				}
				if err := workflow.ValidateJSON(raw, json.RawMessage(workflowInputSchema)); err != nil {
					return err
				}
				var input workflow.Input
				if err := json.Unmarshal(raw, &input); err != nil {
					return err
				}
				if input.Default != nil {
					if err := recipe.CheckInput(input.Default, input.Type); err != nil {
						return fmt.Errorf("default: %w", err)
					}
				}
				if d.Inputs == nil {
					d.Inputs = map[string]workflow.Input{}
				}
				if edit.Name == "" && len(d.Inputs) >= 32 {
					return fmt.Errorf("recipe permits at most 32 inputs")
				}
				d.Inputs[name] = input
			} else {
				if _, exists := d.Outputs[name]; exists && edit.Name == "" {
					return fmt.Errorf("output name already exists")
				}
				if !json.Valid([]byte(v["value"])) {
					return fmt.Errorf("output value must be valid JSON")
				}
				if d.Outputs == nil {
					d.Outputs = map[string]json.RawMessage{}
				}
				if edit.Name == "" && len(d.Outputs) >= 32 {
					return fmt.Errorf("recipe permits at most 32 outputs")
				}
				d.Outputs[name] = json.RawMessage(v["value"])
			}
			return m.replaceRecipe(d, edit.Draft)
		},
	})
}
