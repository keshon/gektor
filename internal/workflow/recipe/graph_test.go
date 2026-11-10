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
