package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func queuedInvocation(t *testing.T, m *model) workflowInvocation {
	t.Helper()
	action := m.workflows.commands[len(m.workflows.commands)-1]().(workflowAction)
	if action.action != "invoke" {
		t.Fatal(action.action)
	}
	var in workflowInvocation
	if err := json.Unmarshal([]byte(action.value), &in); err != nil {
		t.Fatal(err)
	}
	return in
}

func TestDeclaredRunInputsPreserveTypesDefaultsAndOmissions(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	d.Inputs = map[string]workflow.Input{
		"count": {Type: "integer", Default: json.RawMessage(`9007199254740993`)},
		"text":  {Type: "string", Required: true},
		"empty": {Type: "string", Default: json.RawMessage(`"old"`)},
		"flag":  {Type: "boolean", Required: true},
		"data":  {Type: "object", Required: true},
		"skip":  {Type: "array"},
	}
	w.editor.Replace(d)
	before := w.editor.Source()
	m.workflowCommand("run")
	values := map[string]string{
		"value:count": "9007199254740993",
		"value:text":  `{"$step":"literal string"}`,
		"value:empty": "",
		"value:flag":  "false", "value:data": `{"count":9007199254740993}`,
		"value:skip": "",
	}
	if err := m.applyValues(values); err != nil {
		t.Fatal(err)
	}
	in := queuedInvocation(t, m)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(in.Inputs, &got); err != nil {
		t.Fatal(err)
	}
	if got["count"] != nil || got["skip"] != nil || string(got["empty"]) != `""` || string(got["flag"]) != "false" || string(got["data"]) != `{"count":9007199254740993}` {
		t.Fatal(string(in.Inputs))
	}
	var literal string
	if json.Unmarshal(got["text"], &literal) != nil || literal != values["value:text"] {
		t.Fatal("string interpreted as reference")
	}
	values["value:count"] = "9007199254740995"
	if err := m.applyValues(values); err != nil {
		t.Fatal(err)
	}
	in = queuedInvocation(t, m)
	if !strings.Contains(string(in.Inputs), `"count":9007199254740995`) {
		t.Fatal(string(in.Inputs))
	}
	values["value:count"] = "1.5"
	if err := m.applyValues(values); err == nil {
		t.Fatal("fraction accepted")
	}
	if w.editor.Source() != before {
		t.Fatal("invocation mutated definition")
	}
	d.Name = "Changed"
	w.editor.Replace(d)
	action := m.workflows.commands[len(m.workflows.commands)-1]().(workflowAction)
	if cmd := m.workflows.action(action); cmd != nil || w.busy || !strings.Contains(w.notice, "Draft changed") {
		t.Fatal("stale invocation executed")
	}
}

func TestJSONRunInputsValidateBeforeSubmission(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	d.Inputs = map[string]workflow.Input{"count": {Type: "integer", Required: true}}
	w.editor.Replace(d)
	m.workflows.inputForm(workflowInvocation{Action: "validate", Draft: w.editor.Source(), Mode: "JSON"})
	for _, raw := range []string{`null`, `{} {}`, `{"count":"wrong"}`, `{"unknown":1}`, `{}`} {
		if err := m.applyValues(map[string]string{"inputs": raw}); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	if err := m.applyValues(map[string]string{"inputs": `{"count":9007199254740993}`}); err != nil {
		t.Fatal(err)
	}
	in := queuedInvocation(t, m)
	if in.Action != "validate" || string(in.Inputs) != `{"count":9007199254740993}` {
		t.Fatal(in)
	}
}

func TestWorkflowValidationFocusesNodeAndArgument(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	second := d.Steps[0]
	second.ID = "second"
	d.Steps = append(d.Steps, second)
	w.editor.Replace(d)
	err := workflow.ValidateJSON(json.RawMessage(`{"input":3}`), json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}}}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
	err = fmt.Errorf("step second: %w", err)
	before := w.editor.Source()
	rows := m.workflows.validationRows(err)
	if w.graph.Selected != "second" || !w.inspect || !strings.Contains(strings.Join(rows, "\n"), "Value: /input") {
		t.Fatal(rows, w.graph.Selected)
	}
	if w.editor.Source() != before {
		t.Fatal("navigation mutated recipe")
	}
	id, _ := workflowErrorLocation(fmt.Errorf("step second/child: failed"), d)
	if id != "second" {
		t.Fatal(id)
	}
	id, _ = workflowErrorLocation(fmt.Errorf("unrelated error"), d)
	if id != "" {
		t.Fatal("invented node", id)
	}
	m.workflows.acceptResult(workflowResult{action: "validate-inputs", err: err})
	if m.chrome.Dialog == nil || w.graph.Selected != "second" {
		t.Fatal("result did not focus error")
	}
}

func TestRecipeSchemaErrorLocationFindsIndexedNode(t *testing.T) {
	d := workflowRecipe()
	second := d.Steps[0]
	second.ID = "second"
	d.Steps = append(d.Steps, second)
	err := workflow.ValidateJSON(json.RawMessage(`{"steps":[{"args":{"value":"ok"}},{"args":{"value":3}}]}`), json.RawMessage(`{"type":"object","properties":{"steps":{"type":"array","items":{"type":"object","properties":{"args":{"type":"object","properties":{"value":{"type":"string"}}}}}}}}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
	id, path := workflowErrorLocation(err, d)
	if id != "second" || path != "/args/value" {
		t.Fatal(id, path)
	}
	id, path = workflowErrorLocation(fmt.Errorf("step second: %w", err), d)
	if id != "second" || path != "/steps/1/args/value" {
		t.Fatal("node-relative path treated as recipe path", id, path)
	}
}

func TestGeneratedInputInvocationExecutesBoundRecipe(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	d.Inputs = map[string]workflow.Input{"number": {Type: "integer", Required: true}}
	d.Steps[0].Args = json.RawMessage(`{"code":"return input + 1;","input":{"$input":"number"}}`)
	w.editor.Replace(d)
	m.workflows.inputForm(workflowInvocation{Action: "run", Mode: "Fields", Draft: w.editor.Source()})
	if err := m.applyValues(map[string]string{"value:number": "7"}); err != nil {
		t.Fatal(err)
	}
	action := m.workflows.commands[len(m.workflows.commands)-1]().(workflowAction)
	m.chrome.Dialog = nil
	cmd := m.workflows.action(action)
	if cmd == nil {
		t.Fatal("invocation not started")
	}
	msg := cmd()
	for {
		if result, ok := msg.(workflowResult); ok {
			if result.err != nil || !strings.Contains(result.text, `"status":"completed"`) {
				t.Fatal(result.err, result.text)
			}
			return
		}
		if id, ok := msg.(workflowRunID); ok {
			w.runID = string(id)
		} else {
			t.Fatalf("unexpected event %T", msg)
		}
		select {
		case msg = <-w.events:
		case <-time.After(5 * time.Second):
			t.Fatal("invocation stalled")
		}
	}
}
