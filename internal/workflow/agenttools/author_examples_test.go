package agenttools

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workspace"
)

func TestWorkflowArgumentExamplesExecute(t *testing.T) {
	examples := workflowArgumentExamples()
	cases := []struct {
		name      string
		example   int
		inputs    map[string]json.RawMessage
		want      string
		wantError string
	}{
		{name: "scalar default", example: 0, want: `{"doubled":6}`},
		{name: "scalar changed", example: 0, inputs: map[string]json.RawMessage{"count": json.RawMessage(`11`)}, want: `{"doubled":22}`},
		{name: "array default", example: 1, want: `{"labels":["start","alpha","beta"]}`},
		{name: "array changed", example: 1, inputs: map[string]json.RawMessage{"labels": json.RawMessage(`["one","two","three"]`)}, want: `{"labels":["start","one","two","three"]}`},
		{name: "array empty", example: 1, inputs: map[string]json.RawMessage{"labels": json.RawMessage(`[]`)}, want: `{"labels":["start"]}`},
		{name: "artifact default", example: 2, want: `{"text":"hello"}`},
		{name: "artifact changed", example: 2, inputs: map[string]json.RawMessage{"text": json.RawMessage(`"Привет\nsecond line"`)}, want: `{"text":"Привет\nsecond line"}`},
		{name: "required supplied", example: 3, inputs: map[string]json.RawMessage{"label": json.RawMessage(`"sample"`)}, want: `{"label":"sample"}`},
		{name: "required empty supplied", example: 3, inputs: map[string]json.RawMessage{"label": json.RawMessage(`""`)}, want: `{"label":""}`},
		{name: "required missing", example: 3, wantError: `required input "label" missing`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			base := t.TempDir()
			registry, err := app.WorkflowRegistry(ws, base, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			recipeArgs(t, ws, examples[tc.example])
			runner := service.Service{WS: ws, StoreDir: base, Registry: registry, Authorize: allow, ValidateOnly: true}
			validated, err := runner.Run(t.Context(), "recipe.json", tc.inputs)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) || validated.Valid {
					t.Fatalf("missing required input passed validation: %+v, %v", validated, err)
				}
				runner.ValidateOnly = false
				result, runErr := runner.Run(t.Context(), "recipe.json", tc.inputs)
				if runErr == nil || !strings.Contains(runErr.Error(), tc.wantError) || result.RunID != "" {
					t.Fatalf("missing required input reached execution: %+v, %v", result, runErr)
				}
				return
			}
			if err != nil || !validated.Valid {
				t.Fatalf("example failed validation: %+v, %v", validated, err)
			}
			runner.ValidateOnly = false
			result, err := runner.Run(t.Context(), "recipe.json", tc.inputs)
			if err != nil || result.Status != "completed" || result.RunID == "" {
				t.Fatalf("example failed execution: %+v, %v", result, err)
			}
			var got, want any
			err = json.Unmarshal(result.Outputs, &got)
			if err != nil {
				t.Fatal(err)
			}
			err = json.Unmarshal([]byte(tc.want), &want)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("declared outputs=%s, want %s", result.Outputs, tc.want)
			}
			for _, step := range result.Steps {
				if step.Status != "completed" {
					t.Fatalf("example left unfinished step: %+v", step)
				}
			}
		})
	}
}
