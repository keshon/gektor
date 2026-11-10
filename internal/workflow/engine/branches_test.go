package engine

import (
	"encoding/json"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
)

func TestSkippedBranchesAndAnyMerge(t *testing.T) {
	d := workflow.Definition{Version: 3, Name: "branches", Steps: []workflow.Step{
		{ID: "no", Args: json.RawMessage(`{}`), When: json.RawMessage(`false`)},
		{ID: "child", Args: json.RawMessage(`{}`), DependsOn: []string{"no"}},
		{ID: "yes", Args: json.RawMessage(`{}`)},
		{ID: "join", Args: json.RawMessage(`{}`), DependsOn: []string{"no", "yes"}, Merge: "any"},
	}}
	h := &fakeHandler{}
	r, err := Run(t.Context(), d, h, func(workflow.Run) error { return nil })
	if err != nil || h.calls != 2 || r.Status != "completed" || r.Steps[1].Status != "skipped" || r.Steps[3].Status != "completed" {
		t.Fatal(r, err, h.calls)
	}
}
