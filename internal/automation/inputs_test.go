package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type pipelineHandler struct {
	calls  []Step
	output string
}

func (h *pipelineHandler) Validate(s Step) error {
	if !HasOutputReferences(s.Args) {
		var args struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(s.Args, &args); err != nil {
			return err
		}
	}
	return nil
}
func (h *pipelineHandler) Run(_ context.Context, s Step) (string, error) {
	h.calls = append(h.calls, s)
	if h.output != "" {
		return h.output, nil
	}
	return `{"items":[{"output":"out/result.png"}],"a/b":{"~key":"escaped"}}`, nil
}

func pipelineDefinition() Definition {
	return Definition{Version: 2, Name: "pipeline", Inputs: map[string]Input{"source": {Type: "string", Required: true}}, Steps: []Step{
		{ID: "first", Tool: "test", Args: json.RawMessage(`{"value":{"$input":"source"}}`)},
		{ID: "second", Tool: "test", Args: json.RawMessage(`{"value":{"$step":"first","pointer":"/items/0/output"}}`)},
	}}
}

func TestInputsAndOutputsAreBoundAndRecorded(t *testing.T) {
	d := pipelineDefinition()
	d.Inputs["source"] = Input{Type: "string", Default: json.RawMessage(`"input.png"`)}
	h := &pipelineHandler{}
	var saved Record
	r, err := RunWithInputs(t.Context(), d, nil, h, func(r Record) error {
		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, &saved)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.calls) != 2 || string(h.calls[1].Args) != `{"value":"out/result.png"}` || r.Status != "completed" {
		t.Fatalf("%+v %+v", r, h.calls)
	}
	if string(saved.Inputs["source"]) != `"input.png"` || string(saved.Steps[1].Args) != string(h.calls[1].Args) {
		t.Fatalf("missing resolved evidence: %+v", saved)
	}
	if !strings.Contains(string(d.Steps[0].Args), "$input") || !strings.Contains(string(r.Definition.Steps[1].Args), "$step") {
		t.Fatal("definition was mutated")
	}
	d.Inputs["source"] = Input{Type: "boolean"}
	d.Steps[0].Args[0] = 'x'
	if r.Definition.Inputs["source"].Type != "string" || r.Definition.Steps[0].Args[0] != '{' {
		t.Fatal("record aliases caller data")
	}
	value, err := resolveOutputs(json.RawMessage(`{"value":{"$step":"first","pointer":"/a~1b/~0key"}}`), map[string]string{"first": `{"a/b":{"~key":"escaped"}}`})
	if err != nil || string(value) != `{"value":"escaped"}` {
		t.Fatalf("pointer: %s %v", value, err)
	}
}

func TestInvalidInputsAndReferencesHaveNoEffects(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Definition)
		values map[string]json.RawMessage
	}{
		{"missing", func(*Definition) {}, nil},
		{"unknown", func(*Definition) {}, map[string]json.RawMessage{"other": json.RawMessage(`"x"`)}},
		{"wrong type", func(*Definition) {}, map[string]json.RawMessage{"source": json.RawMessage(`1`)}},
		{"self", func(d *Definition) { d.Steps[0].Args = json.RawMessage(`{"value":{"$step":"first"}}`) }, nil},
		{"forward", func(d *Definition) { d.Steps[0].Args = json.RawMessage(`{"value":{"$step":"second"}}`) }, nil},
		{"extra reference field", func(d *Definition) { d.Steps[1].Args = json.RawMessage(`{"value":{"$step":"first","typo":true}}`) }, nil},
		{"bad escape", func(d *Definition) { d.Steps[1].Args = json.RawMessage(`{"value":{"$step":"first","pointer":"/a~3"}}`) }, nil},
		{"bad default", func(d *Definition) { d.Inputs["source"] = Input{Type: "string", Default: json.RawMessage(`false`)} }, nil},
		{"reserved input object", func(d *Definition) {
			d.Inputs["source"] = Input{Type: "object", Default: json.RawMessage(`{"$step":"first"}`)}
		}, nil},
		{"v1 inputs", func(d *Definition) { d.Version = 1 }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := pipelineDefinition()
			d.Inputs["source"] = Input{Type: "string", Default: json.RawMessage(`"input.png"`)}
			if tc.name == "missing" {
				d.Inputs["source"] = Input{Type: "string", Required: true}
			}
			tc.change(&d)
			h := &pipelineHandler{}
			saves := 0
			_, err := RunWithInputs(t.Context(), d, tc.values, h, func(Record) error { saves++; return nil })
			if err == nil || len(h.calls) != 0 || saves != 0 {
				t.Fatalf("invalid definition had effects: %v calls=%d saves=%d", err, len(h.calls), saves)
			}
		})
	}
}

func TestBadOutputStopsBeforeDependentOperation(t *testing.T) {
	for _, output := range []string{`{}`, `{"items":[{"output":12}]}`, `not json`, strings.Repeat("x", maxOutput+1)} {
		d := pipelineDefinition()
		d.Steps = append(d.Steps, Step{ID: "later", Tool: "test", Args: json.RawMessage(`{"value":"later"}`)})
		h := &pipelineHandler{output: output}
		r, err := RunWithInputs(t.Context(), d, map[string]json.RawMessage{"source": json.RawMessage(`"input.png"`)}, h, func(Record) error { return nil })
		if err == nil || len(h.calls) != 1 || r.Status != "failed" || r.Steps[2].Status != "skipped" {
			t.Fatalf("bad output executed dependent step: %v %+v", err, r)
		}
	}
}

func TestInputNumberPrecisionAndLimits(t *testing.T) {
	if err := checkInput(json.RawMessage(`9007199254740993`), "integer"); err != nil {
		t.Fatal(err)
	}
	if err := checkInput(json.RawMessage(`1.5`), "integer"); err == nil {
		t.Fatal("fraction accepted as integer")
	}
	d := pipelineDefinition()
	_, _, err := Prepare(d, map[string]json.RawMessage{"source": json.RawMessage(fmt.Sprintf("%q", strings.Repeat("x", maxArguments)))}, &pipelineHandler{})
	if err == nil {
		t.Fatal("oversized input accepted")
	}
}
