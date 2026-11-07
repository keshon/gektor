package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/automation/recipe"
	"github.com/keshon/gektor/internal/tui/components/schemaform"
)

type workflowInvocation struct {
	Action, Draft, Mode string
	Inputs              json.RawMessage
	ScheduleID          string
}

func (m *model) queueInvocation(action string, in workflowInvocation) {
	b, _ := json.Marshal(in)
	m.queueWorkflow(action, string(b))
}

func (m *model) workflowRunInputs(action string) {
	w := m.workflows
	in := workflowInvocation{Action: action, Draft: w.editor.Source(), Mode: "Simple"}
	if len(w.editor.Definition.Inputs) == 0 {
		in.Inputs = json.RawMessage(`{}`)
		m.queueInvocation("invoke", in)
		return
	}
	m.workflowInputForm(in)
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
	if in.Action != "run" && in.Action != "validate" && in.Action != "schedule" {
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
	var scheduled map[string]json.RawMessage
	if in.Action == "schedule" {
		_ = json.Unmarshal(in.Inputs, &scheduled)
	}
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
		if value, ok := scheduled[name]; ok {
			defaults[name] = value
		}
	}
	raw, _ := json.Marshal(defaults)
	var fields []formField
	choices := map[string][]string{}
	initial := map[string]string{}
	for _, field := range schema.Fields(raw) {
		name := field.Key
		spec := specs[name]
		field.Key = "value:" + name
		kind := spec.Type
		switch kind {
		case "string":
			kind = "text"
		case "array":
			kind = "list"
		case "integer", "number":
			kind = "number"
		case "boolean":
			kind = "yes/no"
		}
		field.Label = name + " (" + kind + ")"
		if spec.Required && spec.Default == nil {
			field.Label += " *"
		}
		if spec.Default == nil && !spec.Required {
			field.Label += " / optional"
		}
		if spec.Type == "array" && defaults[name] != nil {
			var items []json.RawMessage
			if json.Unmarshal(defaults[name], &items) == nil && items != nil {
				numeric := true
				var values []string
				for _, item := range items {
					if recipe.CheckInput(item, "number") != nil {
						numeric = false
					}
					values = append(values, string(item))
				}
				if numeric {
					field.Value = strings.Join(values, ", ")
				}
			}
		}
		initial[name] = field.Value
		fields = append(fields, field)
		if spec.Type == "boolean" {
			choices[field.Key] = []string{"false", "true"}
			if !spec.Required && spec.Default == nil {
				choices[field.Key] = []string{"", "false", "true"}
			}
		}
	}
	title := "Run workflow"
	primary := "Run"
	if in.Action == "validate" {
		title = "Check workflow"
		primary = "Check"
	}
	if in.Action == "schedule" {
		title = "Schedule / input values"
		primary = "Next"
	}
	hint := "Defaults are filled in. Edit values for this run; lists accept comma-separated values."
	if in.Action == "schedule" {
		hint = "These values will be saved with the schedule and used for each scheduled run."
	}
	m.openForm(title, hint, primary, fields, func(v map[string]string) error {
		values := map[string]json.RawMessage{}
		for name, spec := range specs {
			text := v["value:"+name]
			if spec.Default != nil && text == initial[name] && in.Action != "schedule" {
				continue
			}
			if spec.Default == nil && !spec.Required && text == "" && scheduled[name] == nil {
				continue
			}
			value := json.RawMessage(text)
			if spec.Type == "string" {
				value, _ = json.Marshal(text)
			}
			if spec.Type == "array" && !strings.HasPrefix(strings.TrimSpace(text), "[") {
				value = json.RawMessage("[" + text + "]")
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
