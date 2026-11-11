package config

import "fmt"

// UnlimitedSteps is the persisted ceiling used for an unlimited direct run.
const UnlimitedSteps = 999999

// Harness stores operator preferences for execution, independently of the model.
type Harness struct {
	MaxSteps         int  `json:"max_steps,omitempty"`
	Steering         bool `json:"steering,omitempty"`
	DelegateSteps    int  `json:"delegate_steps,omitempty"`
	DelegateMaxSteps int  `json:"delegate_max_steps,omitempty"`
}

// DefaultDelegateSteps is the allowance used when delegate steps are omitted.
const DefaultDelegateSteps = 12

// DelegateBudget resolves omitted limits and rejects negative or inverted limits.
func (h Harness) DelegateBudget() (int, int, error) {
	if h.DelegateSteps < 0 || h.DelegateMaxSteps < 0 {
		return 0, 0, fmt.Errorf("delegate step limits must be positive; omit them to use defaults")
	}

	steps := h.DelegateSteps
	maximum := h.DelegateMaxSteps
	if steps == 0 {
		steps = DefaultDelegateSteps
	}

	if maximum == 0 {
		maximum = steps
	}

	if steps > maximum {
		return 0, 0, fmt.Errorf("delegate_steps (%d) exceeds delegate_max_steps (%d)", steps, maximum)
	}

	return steps, maximum, nil
}
