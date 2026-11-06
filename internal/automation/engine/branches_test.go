package engine

import (
	"encoding/json"
	"github.com/keshon/gektor/internal/automation"
	"testing"
)

func TestSkippedBranchesAndAnyMerge(t *testing.T) {
	d := automation.Definition{Version: 3, Name: "branches", Steps: []automation.Step{
		{ID: "no", Args: json.RawMessage(`{}`), When: json.RawMessage(`false`)},
		{ID: "child", Args: json.RawMessage(`{}`), DependsOn: []string{"no"}},
		{ID: "yes", Args: json.RawMessage(`{}`)},
		{ID: "join", Args: json.RawMessage(`{}`), DependsOn: []string{"no", "yes"}, Merge: "any"},
	}}
	h := &fakeHandler{}
	r, err := Run(t.Context(), d, h, func(automation.Run) error { return nil })
	if err != nil || h.calls != 2 || r.Status != "completed" || r.Steps[1].Status != "skipped" || r.Steps[3].Status != "completed" {
		t.Fatal(r, err, h.calls)
	}
}
