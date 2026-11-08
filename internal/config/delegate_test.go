package config

import (
	"encoding/json"
	"testing"
)

func TestDelegateBudgetValidationAndRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		h              Harness
		steps, maximum int
		bad            bool
	}{
		{Harness{}, 12, 12, false}, {Harness{DelegateSteps: 24}, 24, 24, false},
		{Harness{DelegateSteps: 12, DelegateMaxSteps: 36}, 12, 36, false},
		{Harness{DelegateSteps: -1}, 0, 0, true}, {Harness{DelegateMaxSteps: -1}, 0, 0, true},
		{Harness{DelegateSteps: 24, DelegateMaxSteps: 12}, 0, 0, true},
	} {
		steps, maximum, err := tc.h.DelegateBudget()
		if (err != nil) != tc.bad || steps != tc.steps || maximum != tc.maximum {
			t.Fatalf("%+v: %d %d %v", tc.h, steps, maximum, err)
		}
		data, err := json.Marshal(tc.h)
		if err != nil {
			t.Fatal(err)
		}
		var got Harness
		if err := json.Unmarshal(data, &got); err != nil || got != tc.h {
			t.Fatalf("round trip: %+v %v", got, err)
		}
		if err := (Preferences{Version: 1, Harness: tc.h}).Validate(); (err != nil) != tc.bad {
			t.Fatalf("preferences accepted %+v: %v", tc.h, err)
		}
	}
}
