package workflow

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/components/pane"
	"github.com/keshon/gektor/pkg/tui/components/schemaform"
	"github.com/keshon/gektor/pkg/tui/screen"
)

// recipeReferences provides selectable references without requiring JSON syntax.
func (m *Model) recipeReferences(includeOutputs bool) ([]string, map[string]json.RawMessage) {
	var labels []string
	values := map[string]json.RawMessage{}
	for name := range m.editor.Definition.Inputs {
		label := "Recipe input / " + name
		raw, _ := json.Marshal(map[string]string{"$input": name})
		labels = append(labels, label)
		values[label] = raw
	}
	sort.Strings(labels)
	if includeOutputs {
		for _, step := range m.editor.Definition.Steps {
			pointers := []string{""}
			node, err := m.registry.Node(step.Tool)
			if err == nil {
				pointers = append(pointers, workflowOutputPointers(node.OutputSchema)...)
			}
			for _, pointer := range pointers {
				label := "Node output / " + step.ID + " / " + portLabelForDisplay(pointer)
				ref := map[string]string{"$step": step.ID}
				if pointer != "" {
					ref["pointer"] = pointer
				}
				raw, _ := json.Marshal(ref)
				labels = append(labels, label)
				values[label] = raw
			}
		}
	}
	return labels, values
}

func (m *Model) bindRecipeInput() {
	step := m.selectedStep()
	node, err := m.registry.Node(step.Tool)
	if err != nil {
		return
	}

	names := []string{}
	for name := range schemaform.Parse(node.InputSchema).Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	labels, refs := m.recipeReferences(false)
	if len(names) == 0 || len(labels) == 0 {
		m.setNotice(pane.NoticeInfo, "Select a node with arguments and declare a recipe input in Recipe settings.")
		return
	}
	draft := m.editor.Source()
	m.openForm(screen.Form{
		Title:      "Bind recipe input / " + step.ID,
		Hint:       "The input replaces the selected argument. Undo restores its previous value.",
		ApplyLabel: "Bind input",
		Fields: []formField{
			{Key: "argument", Label: "Node argument", Value: names[0]},
			{Key: "input", Label: "Recipe input", Value: labels[0]},
		},
		Choices: map[string][]string{"argument": names, "input": labels},
	}, func(v map[string]string) error {
		if m.editor.Source() != draft {
			return fmt.Errorf("draft changed; reopen the input binding")
		}
		if !slices.Contains(names, v["argument"]) || refs[v["input"]] == nil {
			return fmt.Errorf("choose an argument and a recipe input")
		}
		d := m.editor.Clone()
		for i := range d.Steps {
			if d.Steps[i].ID != step.ID {
				continue
			}
			args, err := updateValue(d.Steps[i].Args, []string{v["argument"]}, refs[v["input"]], false)
			if err != nil {
				return err
			}
			d.Steps[i].Args = args
			return m.replaceRecipe(d, draft)
		}
		return fmt.Errorf("node is no longer in the recipe")
	})
}

type workflowParameterEdit struct {
	Kind, Name, Draft string
	Remove            bool
}

const workflowInputSchema = `{"type":"object","properties":{"type":{"type":"string","enum":["string","integer","number","boolean","array","object"]},"required":{"type":"boolean","enum":[false,true]},"default":{}},"required":["type"],"additionalProperties":false}`

func (m *Model) recipeName() {
	draft := m.editor.Source()
	m.openForm(screen.Form{
		Title:      "Recipe name",
		Hint:       "This name identifies the recipe in saved runs.",
		ApplyLabel: "Apply",
		Fields:     []formField{{Key: "name", Label: "Name", Value: m.editor.Definition.Name}},
	}, func(v map[string]string) error {
		if strings.TrimSpace(v["name"]) == "" || len(v["name"]) > 120 {
			return fmt.Errorf("name must contain 1–120 bytes")
		}
		d := m.editor.Clone()
		d.Name = v["name"]
		return m.replaceRecipe(d, draft)
	})
}

func (m *Model) replaceRecipe(d workflow.Definition, draft string) error {
	if m.busy || m.run != nil {
		return fmt.Errorf("return to an idle editable draft first")
	}
	if m.editor.Source() != draft {
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
		m.editor.Replace(d)
		m.graph = m.buildGraph(m.graph)
	}
	m.setNotice(pane.NoticeSuccess, "Recipe updated"+screen.Separator()+"V validates the draft")
	return nil
}

func (m *Model) parameters(kind string) {
	var names []string
	if kind == "inputs" {
		for name := range m.editor.Definition.Inputs {
			names = append(names, name)
		}
	} else {
		for name := range m.editor.Definition.Outputs {
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
	draft := m.editor.Source()
	m.openForm(screen.Form{
		Title:      "Recipe / " + kind,
		Hint:       "Add, edit or remove one named value. Existing names stay fixed; references are not rewritten.",
		ApplyLabel: "Next",
		Fields: []formField{
			{Key: "entry", Label: "Entry", Value: options[0]},
			{Key: "action", Label: "Action", Value: "Edit"},
		},
		Choices: map[string][]string{"entry": options, "action": {"Edit", "Remove"}},
	}, func(v map[string]string) error {
		if m.editor.Source() != draft {
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

		change := workflowParameterEdit{Kind: kind, Name: name, Draft: draft, Remove: v["action"] == "Remove"}
		b, _ := json.Marshal(change)
		m.queueAction("parameter-edit", string(b))
		return nil
	})

}

func (m *Model) parameterEdit(edit workflowParameterEdit) {
	if m.busy || m.run != nil {
		return
	}
	if edit.Kind != "inputs" && edit.Kind != "outputs" {
		return
	}
	if m.editor.Source() != edit.Draft {
		m.setNotice(pane.NoticeWarning, "Draft changed"+screen.Separator()+"reopen recipe settings")
		return
	}
	d := m.editor.Clone()
	if edit.Remove {
		var raw []byte
		if edit.Kind == "inputs" {
			raw, _ = json.Marshal(d.Inputs[edit.Name])
		} else {
			raw = d.Outputs[edit.Name]
		}
		m.openForm(screen.Form{
			Title:         "Remove " + edit.Name,
			Hint:          "References to this name are not rewritten. Validate the recipe after removal.",
			ApplyLabel:    "Remove",
			Fields:        []formField{{Key: "current", Label: "Current " + edit.Kind + " value", Value: cleanText(string(raw)), ReadOnly: true, Multiline: true}},
			Destructive:   true,
			StartOnCancel: true,
		}, func(_ map[string]string) error {
			if edit.Kind == "inputs" {
				delete(d.Inputs, edit.Name)
			} else {
				delete(d.Outputs, edit.Name)
			}
			return m.replaceRecipe(d, edit.Draft)
		})
		return
	}
	fields := []formField{{Key: "name", Label: "Name", Value: edit.Name, ReadOnly: edit.Name != ""}}
	schema := schemaform.Parse(json.RawMessage(workflowInputSchema))
	var original json.RawMessage
	var choices map[string][]string
	var references map[string]json.RawMessage
	var visibility map[string][]form.Condition
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
		labels, refs := m.recipeReferences(true)
		references = refs
		if len(labels) > 0 {
			fields = append(fields, formField{Key: "source", Label: "Output source", Value: "Enter JSON"})
			choices = map[string][]string{"source": append([]string{"Enter JSON"}, labels...)}
			visibility = map[string][]form.Condition{"value": {{Key: "source", Value: "Enter JSON"}}}
		}
		fields = append(fields, formField{Key: "value", Label: "Value (when entering JSON)", Value: string(value), Multiline: true})
	}
	hint := "Default is a JSON value; leave it blank for no default. Required inputs can have defaults."
	if edit.Kind == "outputs" {
		hint = "Choose a recipe input or node output. Enter JSON for a literal or a custom reference."
	}
	m.openForm(screen.Form{
		Title:      "Recipe / " + edit.Kind,
		Hint:       hint,
		ApplyLabel: "Apply",
		Fields:     fields,
		Choices:    choices,
		ShowWhen:   visibility,
	}, func(v map[string]string) error {
		d := m.editor.Clone()
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
			validationErr := workflow.ValidateJSON(raw, json.RawMessage(workflowInputSchema))
			if validationErr != nil {
				return validationErr
			}
			var input workflow.Input
			decodeErr := json.Unmarshal(raw, &input)
			if decodeErr != nil {
				return decodeErr
			}
			if input.Default != nil {
				defaultErr := recipe.CheckInput(input.Default, input.Type)
				if defaultErr != nil {
					return fmt.Errorf("default: %w", defaultErr)
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
			value := json.RawMessage(v["value"])
			if source := v["source"]; source != "" && source != "Enter JSON" {
				value = references[source]
				if value == nil {
					return fmt.Errorf("choose an output source")
				}
			}
			if !json.Valid(value) {
				return fmt.Errorf("output value must be valid JSON")
			}
			if d.Outputs == nil {
				d.Outputs = map[string]json.RawMessage{}
			}
			if edit.Name == "" && len(d.Outputs) >= 32 {
				return fmt.Errorf("recipe permits at most 32 outputs")
			}
			d.Outputs[name] = value
		}
		return m.replaceRecipe(d, edit.Draft)
	})
}
