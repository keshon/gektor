package recipe

import (
	"fmt"

	"github.com/keshon/gektor/internal/workflow"
)

func ValidateRepeat(s workflow.Step) error {
	p := s.Repeat
	if s.Tool != "workflow_call" || s.Child == nil {
		return fmt.Errorf("repeat requires a frozen workflow_call")
	}
	if len(s.ForEach) > 0 || s.Concurrency != 0 || s.OnError != "" || s.Retry != nil {
		return fmt.Errorf("repeat cannot combine with for_each, concurrency, on_error or retry")
	}
	if p.MaxIterations < 1 || p.MaxIterations > workflow.MaxIterations {
		return fmt.Errorf("repeat max_iterations must be 1–%d", workflow.MaxIterations)
	}
	if p.DelayMS < 0 || p.DelayMS > 10000 {
		return fmt.Errorf("repeat delay_ms must be 0–10000")
	}
	for _, pointer := range []string{p.Until, p.NextInputs} {
		_, err := pointerParts(pointer)
		if err != nil {
			return fmt.Errorf("repeat pointer: %w", err)
		}
	}
	return nil
}
