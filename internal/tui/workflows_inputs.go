package tui

import (
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/automation/recipe"
	"github.com/keshon/gektor/internal/tui/components/schemaform"
)

type workflowInvocation struct {
	Action, Draft, Mode string
	Inputs              json.RawMessage
}

func (m *model) queueInvocation(action string, in workflowInvocation) {
	b, _ := json.Marshal(in)
	m.queueWorkflow(action, string(b))
}

func (m *model) workflowRunInputs(action string) {
	w := m.workflows
	in := workflowInvocation{Action: action, Draft: w.editor.Source(), Mode: "JSON"}
	if len(w.editor.Definition.Inputs) == 0 {
		m.workflowInputForm(in)
		return
	}
	m.openForm("Workflow / "+action, "Choose fields generated from recipe inputs, or a JSON object.", "Next", []formField{{Key: "format", Label: "Input format", Value: "Fields"}}, func(v map[string]string) error {
		if v["format"] != "Fields" && v["format"] != "JSON" {
			return fmt.Errorf("choose Fields or JSON")
		}
		in.Mode = v["format"]
		m.queueInvocation("input-form", in)
		return nil
	})
	m.dialog.form.Choices = map[string][]string{"format": {"Fields", "JSON"}}
}

func (m *model) workflowInputForm(in workflowInvocation) {
	w := m.workflows
	if w.busy || w.run != nil {
		return
	}
	if in.Draft != w.editor.Source() {
		w.notice = "Draft changed · reopen run inputs"
		return
	}
	if in.Action != "run" && in.Action != "validate" {
		return
	}
	specs := w.editor.Clone().Inputs
	submit := func(values map[string]json.RawMessage) error {
		if w.busy || w.run != nil || w.editor.Source() != in.Draft {
			return fmt.Errorf("draft changed; reopen run inputs")
		}
		if _, err := recipe.BindInputs(specs, values); err != nil {
			return err
		}
		in.Inputs, _ = json.Marshal(values)
		m.queueInvocation("invoke", in)
		return nil
	}
	if in.Mode == "JSON" {
		m.openForm("Workflow / "+in.Action, "Input values are checked before execution; omitted keys use recipe defaults.", "Continue", []formField{{Key: "inputs", Label: "Input values (JSON object)", Value: "{}", Multiline: true}}, func(v map[string]string) error {
			var values map[string]json.RawMessage
			if err := json.Unmarshal([]byte(v["inputs"]), &values); err != nil {
				return err
			}
			if values == nil {
				return fmt.Errorf("inputs must be an object")
			}
			return submit(values)
		})
		return
	}
	schema := schemaform.Schema{Properties: map[string]schemaform.Property{}}
	defaults := map[string]json.RawMessage{}
	for name, spec := range specs {
		kind, _ := json.Marshal(spec.Type)
		property := schemaform.Property{Type: kind}
		if spec.Type == "boolean" {
			property.Enum = []json.RawMessage{json.RawMessage(`false`), json.RawMessage(`true`)}
		}
		schema.Properties[name] = property
		if spec.Default != nil {
			defaults[name] = spec.Default
		}
	}
	raw, _ := json.Marshal(defaults)
	var fields []formField
	choices := map[string][]string{}
	for _, field := range schema.Fields(raw) {
		name := field.Key
		spec := specs[name]
		if !spec.Required || spec.Default != nil {
			mode := "Omit"
			options := []string{"Omit", "Provide"}
			if spec.Default != nil {
				mode = "Use default"
				options = []string{"Use default", "Provide"}
			}
			fields = append(fields, formField{Key: "mode:" + name, Label: name + " / value source", Value: mode})
			choices["mode:"+name] = options
		}
		field.Key = "value:" + name
		if spec.Required && spec.Default == nil {
			field.Label += " *"
		}
		fields = append(fields, field)
		if spec.Type == "boolean" {
			choices[field.Key] = []string{"false", "true"}
		}
	}
	m.openForm("Workflow / "+in.Action, "Strings are literal text; other types use JSON. Choose Provide to override a default, including with an empty string.", "Continue", fields, func(v map[string]string) error {
		values := map[string]json.RawMessage{}
		for name, spec := range specs {
			mode := "Provide"
			if !spec.Required || spec.Default != nil {
				mode = v["mode:"+name]
			}
			switch mode {
			case "Use default":
				if spec.Default == nil {
					return fmt.Errorf("%s has no default", name)
				}
				continue
			case "Omit":
				continue
			case "Provide":
			default:
				return fmt.Errorf("%s: choose a value source", name)
			}
			value := json.RawMessage(v["value:"+name])
			if spec.Type == "string" {
				value, _ = json.Marshal(v["value:"+name])
			}
			if err := recipe.CheckInput(value, spec.Type); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			values[name] = value
		}
		return submit(values)
	})
	m.dialog.form.Choices = choices
}
