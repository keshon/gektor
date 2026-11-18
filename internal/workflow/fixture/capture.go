package fixture

import (
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/workflow"
)

// Capture exports completed invocations; it does not infer unfinished outcomes.
// Outputs are supplied by the workflow owner that collects declared recipe outputs.
func Capture(run workflow.Run, outputs json.RawMessage) ([]byte, error) {
	if run.Status != "completed" || len(run.Recoveries) != 0 {
		return nil, fmt.Errorf("capture requires a completed run without operator recovery")
	}
	spec := specification{
		Version: 1,
		Inputs:  run.Inputs,
		Calls:   []call{},
		Expect:  expectation{Status: "completed", Outputs: outputs},
	}
	for _, scoped := range workflow.ScopedSteps(run) {
		step := scoped.Result
		if scoped.Definition.Review || scoped.Definition.Tool == "workflow_wait" || scoped.Definition.Tool == "workflow_wait_callback" {
			return nil, fmt.Errorf("fixture capture does not model reviews or durable waits")
		}
		if Pure(scoped.Definition.Tool) || len(scoped.Definition.ForEach) > 0 || step.Iterations != nil || step.Status == "skipped" {
			continue
		}
		if step.Status != "completed" || !json.Valid([]byte(step.Output)) {
			return nil, fmt.Errorf("cannot capture incomplete or non-JSON result for %s", scoped.ID)
		}

		version := scoped.Definition.NodeVersion
		if version == 0 {
			version = 1
		}
		call := call{
			ID:        scoped.ID,
			Operation: scoped.Definition.Tool,
			Version:   version,
			Args:      step.Args,
		}

		for index, attempt := range step.Attempts {
			if attempt.Finished == nil {
				return nil, fmt.Errorf("cannot capture unfinished attempt for %s", scoped.ID)
			}
			if index == len(step.Attempts)-1 {
				if attempt.Failure != nil || attempt.Error != "" {
					return nil, fmt.Errorf("completed invocation %s has a failed final attempt", scoped.ID)
				}
				break
			}
			if attempt.Failure == nil {
				return nil, fmt.Errorf("cannot infer an earlier successful or untyped attempt for %s", scoped.ID)
			}
			call.Responses = append(call.Responses, response{Failure: attempt.Failure})
		}
		call.Responses = append(call.Responses, response{Output: json.RawMessage(step.Output)})
		spec.Calls = append(spec.Calls, call)
	}

	raw, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return nil, err
	}
	_, err = New(raw)
	return raw, err
}
