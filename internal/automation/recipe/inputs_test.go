package recipe_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/engine"
	"github.com/keshon/gektor/internal/automation/recipe"
	"strings"
	"testing"
)

type pipelineHandler struct {
	calls  []automation.Step
	output string
}

func (h *pipelineHandler) Validate(s automation.Step) error {
	if !recipe.HasOutputReferences(s.Args) {
		var args struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(s.Args, &args); err != nil {
			return err
		}
	}
	return nil
}
func (h *pipelineHandler) Run(_ context.Context, s automation.Step) (string, error) {
	h.calls = append(h.calls, s)
	if h.output != "" {
		return h.output, nil
	}
	return `{"items":[{"output":"out/result.png"}],"a/b":{"~key":"escaped"}}`, nil
}

func pipelineDefinition() automation.Definition {
	return automation.Definition{Version: 3, Name: "pipeline", Inputs: map[string]automation.Input{"source": {Type: "string", Required: true}}, Steps: []automation.Step{
		{ID: "first", Tool: "test", Args: json.RawMessage(`{"value":{"$input":"source"}}`)},
		{ID: "second", Tool: "test", Args: json.RawMessage(`{"value":{"$step":"first","pointer":"/items/0/output"}}`)},
	}}
}

func TestInputsAndOutputsAreBoundAndRecorded(t *testing.T) {
	d := pipelineDefinition()
	d.Inputs["source"] = automation.Input{Type: "string", Default: json.RawMessage(`"input.png"`)}
	h := &pipelineHandler{}
	var saved automation.Run
	r, err := engine.RunWithInputs(t.Context(), d, nil, h, func(r automation.Run) error {
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
	d.Inputs["source"] = automation.Input{Type: "boolean"}
	d.Steps[0].Args[0] = 'x'
	if r.Definition.Inputs["source"].Type != "string" || r.Definition.Steps[0].Args[0] != '{' {
		t.Fatal("record aliases caller data")
	}
	value, err := recipe.ResolveOutputs(json.RawMessage(`{"value":{"$step":"first","pointer":"/a~1b/~0key"}}`), map[string]string{"first": `{"a/b":{"~key":"escaped"}}`})
	if err != nil || string(value) != `{"value":"escaped"}` {
		t.Fatalf("pointer: %s %v", value, err)
	}
}

func TestInvalidInputsAndReferencesHaveNoEffects(t *testing.T) {
	cases := []struct {
		name   string
		change func(*automation.Definition)
		values map[string]json.RawMessage
	}{
		{"missing", func(*automation.Definition) {}, nil},
		{"unknown", func(*automation.Definition) {}, map[string]json.RawMessage{"other": json.RawMessage(`"x"`)}},
		{"wrong type", func(*automation.Definition) {}, map[string]json.RawMessage{"source": json.RawMessage(`1`)}},
		{"self", func(d *automation.Definition) { d.Steps[0].Args = json.RawMessage(`{"value":{"$step":"first"}}`) }, nil},
		{"forward", func(d *automation.Definition) { d.Steps[0].Args = json.RawMessage(`{"value":{"$step":"second"}}`) }, nil},
		{"extra reference field", func(d *automation.Definition) {
			d.Steps[1].Args = json.RawMessage(`{"value":{"$step":"first","typo":true}}`)
		}, nil},
		{"bad escape", func(d *automation.Definition) {
			d.Steps[1].Args = json.RawMessage(`{"value":{"$step":"first","pointer":"/a~3"}}`)
		}, nil},
		{"bad default", func(d *automation.Definition) {
			d.Inputs["source"] = automation.Input{Type: "string", Default: json.RawMessage(`false`)}
		}, nil},
		{"reserved input object", func(d *automation.Definition) {
			d.Inputs["source"] = automation.Input{Type: "object", Default: json.RawMessage(`{"$step":"first"}`)}
		}, nil},
		{"v1 inputs", func(d *automation.Definition) { d.Version = 1 }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := pipelineDefinition()
			d.Inputs["source"] = automation.Input{Type: "string", Default: json.RawMessage(`"input.png"`)}
			if tc.name == "missing" {
				d.Inputs["source"] = automation.Input{Type: "string", Required: true}
			}
			tc.change(&d)
			h := &pipelineHandler{}
			saves := 0
			_, err := engine.RunWithInputs(t.Context(), d, tc.values, h, func(automation.Run) error { saves++; return nil })
			if err == nil || len(h.calls) != 0 || saves != 0 {
				t.Fatalf("invalid definition had effects: %v calls=%d saves=%d", err, len(h.calls), saves)
			}
		})
	}
}

func TestBadOutputStopsBeforeDependentOperation(t *testing.T) {
	for _, output := range []string{`{}`, `{"items":[{"output":12}]}`, `not json`, strings.Repeat("x", recipe.MaxOutput+1)} {
		d := pipelineDefinition()
		d.Steps = append(d.Steps, automation.Step{ID: "later", Tool: "test", Args: json.RawMessage(`{"value":"later"}`)})
		h := &pipelineHandler{output: output}
		r, err := engine.RunWithInputs(t.Context(), d, map[string]json.RawMessage{"source": json.RawMessage(`"input.png"`)}, h, func(automation.Run) error { return nil })
		if err == nil || len(h.calls) != 1 || r.Status != "failed" || r.Steps[2].Status != "skipped" {
			t.Fatalf("bad output executed dependent step: %v %+v", err, r)
		}
	}
}

func TestInputNumberPrecisionAndLimits(t *testing.T) {
	if err := recipe.CheckInput(json.RawMessage(`9007199254740993`), "integer"); err != nil {
		t.Fatal(err)
	}
	if err := recipe.CheckInput(json.RawMessage(`1.5`), "integer"); err == nil {
		t.Fatal("fraction accepted as integer")
	}
	d := pipelineDefinition()
	_, _, err := recipe.Prepare(d, map[string]json.RawMessage{"source": json.RawMessage(fmt.Sprintf("%q", strings.Repeat("x", recipe.MaxArguments)))}, &pipelineHandler{})
	if err == nil {
		t.Fatal("oversized input accepted")
	}
}
