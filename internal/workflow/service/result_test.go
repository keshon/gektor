package service

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestResultPreservesExternalJSONShapes(t *testing.T) {
	for _, test := range []struct {
		name   string
		result Result
		want   string
	}{
		{
			name:   "validation",
			result: Result{Valid: true, RecipePath: "recipe.json", RecipeSHA256: "digest", Scope: "structure"},
			want:   `{"valid":true,"recipe_path":"recipe.json","recipe_sha256":"digest","scope":"structure"}`,
		},
		{
			name:   "run before any completed steps",
			result: Result{RunID: "run", DatabasePath: "runs.db", Status: "running", RecipeSHA256: "digest", Steps: []StepSummary{}},
			want:   `{"run_id":"run","database_path":"runs.db","status":"running","recipe_sha256":"digest","steps":[]}`,
		},
		{
			name:   "continuation",
			result: Result{RunID: "run", Status: "completed", Outputs: json.RawMessage(`{"value":42}`)},
			want:   `{"run_id":"run","status":"completed","outputs":{"value":42}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.result)
			if err != nil {
				t.Fatal(err)
			}
			var got, want map[string]json.RawMessage
			err = json.Unmarshal(encoded, &got)
			if err != nil {
				t.Fatal(err)
			}
			err = json.Unmarshal([]byte(test.want), &want)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("wire shape changed: got %s, want %s", encoded, test.want)
			}
		})
	}
}
