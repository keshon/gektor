package recipe

import (
	"encoding/json"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
)

func TestGraphOrderAndCycles(t *testing.T) {
	steps := []workflow.Step{{ID: "consumer", Args: json.RawMessage(`{"value":{"$step":"producer"}}`)}, {ID: "producer", Args: json.RawMessage(`{}`)}}
	ordered, err := Order(steps)
	if err != nil || ordered[0].ID != "producer" || ordered[1].DependsOn[0] != "producer" {
		t.Fatal(ordered, err)
	}
	steps[1].DependsOn = []string{"consumer"}
	if _, err := Order(steps); err == nil {
		t.Fatal("cycle accepted")
	}
	steps[1].DependsOn = []string{"missing"}
	if _, err := Order(steps); err == nil {
		t.Fatal("unknown dependency accepted")
	}
}

func TestFailureRoutesAndFinalizerOrdering(t *testing.T) {
	steps := []workflow.Step{
		{ID: "finish", Finally: true, Args: json.RawMessage(`{}`)},
		{ID: "report", OnFailure: "fetch", Args: json.RawMessage(`{}`)},
		{ID: "fetch", Args: json.RawMessage(`{}`)},
		{ID: "@outputs", CollectOutputs: true, Args: json.RawMessage(`{}`), DependsOn: []string{"fetch", "report", "finish"}},
	}
	ordered, err := Order(steps)
	if err != nil || ordered[0].ID != "fetch" || ordered[1].ID != "report" || ordered[2].ID != "finish" || ordered[3].ID != "@outputs" {
		t.Fatal(ordered, err)
	}
	for _, invalid := range [][]workflow.Step{
		{{ID: "report", OnFailure: "missing"}},
		{{ID: "report", OnFailure: "report"}},
		{{ID: "finish", Finally: true}, {ID: "report", OnFailure: "finish"}},
		{{ID: "fetch"}, {ID: "report", OnFailure: "fetch"}, {ID: "another", OnFailure: "report"}},
		{{ID: "fetch"}, {ID: "report", Finally: true, OnFailure: "fetch"}},
		{{ID: "finish", Finally: true}, {ID: "fetch", DependsOn: []string{"finish"}}},
	} {
		_, err = Order(invalid)
		if err == nil {
			t.Fatal("invalid failure/finalizer graph accepted", invalid)
		}
	}
}
