package config

import (
	"encoding/json"
	"testing"
)

func TestDelegateBudgetValidationAndRoundTrip(t *testing.T) {
	cases := []struct {
		harness Harness
		steps   int
		maximum int
		invalid bool
	}{
		{harness: Harness{}, steps: 12, maximum: 12},
		{harness: Harness{DelegateSteps: 24}, steps: 24, maximum: 24},
		{
			harness: Harness{DelegateSteps: 12, DelegateMaxSteps: 36},
			steps:   12,
			maximum: 36,
		},
		{harness: Harness{DelegateSteps: -1}, invalid: true},
		{harness: Harness{DelegateMaxSteps: -1}, invalid: true},
		{
			harness: Harness{DelegateSteps: 24, DelegateMaxSteps: 12},
			invalid: true,
		},
	}

	for _, tc := range cases {
		steps, maximum, err := tc.harness.DelegateBudget()
		if (err != nil) != tc.invalid || steps != tc.steps || maximum != tc.maximum {
			t.Fatalf("%+v: %d %d %v", tc.harness, steps, maximum, err)
		}

		data, err := json.Marshal(tc.harness)
		if err != nil {
			t.Fatal(err)
		}

		var got Harness
		err = json.Unmarshal(data, &got)
		if err != nil || got != tc.harness {
			t.Fatalf("round trip: %+v %v", got, err)
		}

		preferences := Preferences{Version: 1, Harness: tc.harness}
		err = preferences.Validate()
		if (err != nil) != tc.invalid {
			t.Fatalf("preferences accepted %+v: %v", tc.harness, err)
		}
	}
}
