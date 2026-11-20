package workflow

import (
	"encoding/json"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/components/pane"
	"github.com/keshon/gektor/pkg/tui/components/schemaform"
	"github.com/keshon/gektor/pkg/tui/screen"
)

type behaviorEdit struct {
	StepID, Section, Draft string
}

func (m *Model) behaviorForm() tea.Cmd {
	step := m.selectedStep()
	if step.ID == "" {
		return nil
	}

	draft := m.editor.Source()
	m.openForm(screen.Form{
		Title:      step.ID + " / behavior",
		Hint:       "Choose which settings to edit.",
		ApplyLabel: "Open",
		Fields:     []formField{{Key: "section", Label: "Section", Value: "Execution"}},
		Choices:    map[string][]string{"section": {"Execution", "Map", "Loop", "Retry"}},
	}, func(v map[string]string) error {
		if m.editor.Source() != draft {
			return fmt.Errorf("draft changed; reopen node behavior")
		}
		edit := behaviorEdit{StepID: step.ID, Section: v["section"], Draft: draft}
		raw, err := json.Marshal(edit)
		if err != nil {
			return err
		}
		m.queueAction("behavior-edit", string(raw))
		return nil
	})

	return nil
}

func (m *Model) editBehavior(edit behaviorEdit) {
	if m.run != nil || m.editor.Source() != edit.Draft {
		m.setNotice(pane.NoticeWarning, "Draft changed · reopen node behavior")
		return
	}
	d := m.editor.Clone()
	index := -1
	for i, step := range d.Steps {
		if step.ID == edit.StepID {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	step := d.Steps[index]

	var source any = step
	var schemaJSON, hint string
	var labels [][2]string
	enabled := true
	switch edit.Section {
	case "Execution":
		schemaJSON = `{"properties":{
			"depends_on":{"type":"array"}, "merge":{"type":"string","enum":["all","any"]},
			"when":{}, "review":{"type":"boolean","enum":[false,true]},
			"on_failure":{"type":"string"}, "finally":{"type":"boolean","enum":[false,true]}
		}}`
		hint = "This node can handle another node’s operation failure. Finalizers run after success or operation failure, never after cancellation or a permission denial."
		labels = [][2]string{
			{"depends_on", "After nodes (JSON array)"},
			{"merge", "Wait for all / any"},
			{"when", "Condition (JSON or reference)"},
			{"review", "Require review"},
			{"on_failure", "Handle failure of node (blank: none)"},
			{"finally", "Finalizer"},
		}
	case "Map":
		schemaJSON = `{"properties":{
			"for_each":{}, "concurrency":{"type":"integer"},
			"on_error":{"type":"string","enum":["stop","collect"]}
		}}`
		hint = "Each item supplies the operation arguments. Concurrency is 1–32; child workflows stay serial. Collect retains operation failures. Clear items to disable the map."
		labels = [][2]string{
			{"for_each", "Items (JSON array or reference)"},
			{"concurrency", "Concurrency (blank: 1)"},
			{"on_error", "Item failure"},
		}
	case "Loop":
		schemaJSON = `{"properties":{
			"max_iterations":{"type":"integer"}, "until":{"type":"string"},
			"next_inputs":{"type":"string"}, "delay_ms":{"type":"integer"}
		},"required":["max_iterations","until","next_inputs"]}`
		hint = "Workflow calls only. Until points to a boolean output; next inputs points to an object. Blank pointers select the whole output. Validate before running."
		labels = [][2]string{
			{"max_iterations", "Maximum iterations (1–10000)"},
			{"until", "Until (JSON Pointer)"},
			{"next_inputs", "Next inputs (JSON Pointer)"},
			{"delay_ms", "Delay in ms (0–10000)"},
		}
		enabled = step.Repeat != nil
		source = step.Repeat
		if !enabled {
			source = workflow.RepeatPolicy{MaxIterations: 10, Until: "/done", NextInputs: "/next"}
		}
	case "Retry":
		schemaJSON = `{"properties":{
			"max_attempts":{"type":"integer"}, "codes":{"type":"array"}, "delay_ms":{"type":"integer"}
		},"required":["max_attempts"]}`
		hint = "Retry-safe operations only. Leave codes blank to retry any operation failure. Permission, validation and storage failures are never retried."
		labels = [][2]string{
			{"max_attempts", "Maximum attempts (1–3)"},
			{"codes", "Failure codes (JSON array)"},
			{"delay_ms", "Delay in ms (0–10000)"},
		}
		enabled = step.Retry != nil
		source = step.Retry
		if !enabled {
			source = workflow.RetryPolicy{MaxAttempts: 2}
		}
	default:
		return
	}

	original, err := json.Marshal(source)
	if err != nil {
		m.setNotice(pane.NoticeError, err.Error())
		return
	}
	schema := schemaform.Parse(json.RawMessage(schemaJSON))
	generated := schema.Fields(original)
	fields := []formField{}
	choices := schema.Choices()
	showWhen := map[string][]form.Condition{}
	optional := edit.Section == "Loop" || edit.Section == "Retry"
	if optional {
		fields = append(fields, formField{Key: "enabled", Label: "Enabled", Value: fmt.Sprint(enabled)})
		choices["enabled"] = []string{"false", "true"}
	}
	for _, label := range labels {
		for _, field := range generated {
			if field.Key != label[0] {
				continue
			}
			field.Label = label[1]
			fields = append(fields, field)
			if optional {
				showWhen[field.Key] = []form.Condition{{Key: "enabled", Value: "true"}}
			}
		}
	}
	if edit.Section == "Execution" {
		targets := []string{""}
		for _, target := range d.Steps {
			if target.ID != step.ID && !target.Finally {
				targets = append(targets, target.ID)
			}
		}
		choices["on_failure"] = targets
	}

	m.openForm(screen.Form{
		Title:      step.ID + " / " + edit.Section,
		Hint:       hint,
		ApplyLabel: "Apply",
		Fields:     fields,
		Choices:    choices,
		ShowWhen:   showWhen,
	}, func(v map[string]string) error {
		if m.busy || m.run != nil || m.editor.Source() != edit.Draft {
			return fmt.Errorf("draft changed; reopen node behavior")
		}

		changed := step
		if optional && v["enabled"] == "false" {
			if edit.Section == "Loop" {
				changed.Repeat = nil
			} else {
				changed.Retry = nil
			}
		} else {
			raw, valuesErr := schema.Values(original, v)
			if valuesErr != nil {
				return valuesErr
			}
			switch edit.Section {
			case "Loop":
				changed.Repeat = nil
				err = json.Unmarshal(raw, &changed.Repeat)
			case "Retry":
				changed.Retry = nil
				err = json.Unmarshal(raw, &changed.Retry)
			default:
				changed = workflow.Step{}
				err = json.Unmarshal(raw, &changed)
			}
			if err != nil {
				return err
			}
		}

		if edit.Section == "Map" && len(changed.ForEach) == 0 {
			changed.Concurrency = 0
			changed.OnError = ""
		}

		d.Steps[index] = changed
		return m.replaceRecipe(d, edit.Draft)
	})
}
