package recipe

import (
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/workflow"
)

// Order produces a stable topological order. Data references imply dependencies;
// explicit edges also order operations whose only relationship is a side effect.
func Order(steps []workflow.Step) ([]workflow.Step, error) {
	byID := map[string]workflow.Step{}

	for _, s := range steps {
		if s.ID == "" || len(s.ID) > 64 {
			return nil, fmt.Errorf("invalid step ID")
		}

		if _, ok := byID[s.ID]; ok {
			return nil, fmt.Errorf("duplicate step %s", s.ID)
		}

		byID[s.ID] = s
	}

	for _, s := range steps {
		if s.Merge != "" && s.Merge != "all" && s.Merge != "any" {
			return nil, fmt.Errorf("step %s: merge must be all or any", s.ID)
		}

		deps := append([]string(nil), s.DependsOn...)

		for _, raw := range []json.RawMessage{s.Args, s.When, s.ForEach} {
			if len(raw) == 0 {
				continue
			}

			_, err := bind(raw, func(ref map[string]any) (any, error) {
				if _, ok := ref["$step"]; ok {
					name, _, err := outputReference(ref)
					if err != nil {
						return nil, err
					}

					deps = append(deps, name)
				}

				return ref, nil
			})
			if err != nil {
				return nil, err
			}
		}

		seen := map[string]bool{}
		s.DependsOn = nil

		for _, id := range deps {
			if id == s.ID {
				return nil, fmt.Errorf("step %s depends on itself", id)
			}

			if _, ok := byID[id]; !ok {
				return nil, fmt.Errorf("unknown dependency %s", id)
			}

			if !seen[id] {
				s.DependsOn = append(s.DependsOn, id)
				seen[id] = true
			}
		}

		byID[s.ID] = s
	}

	done := map[string]bool{}
	out := make([]workflow.Step, 0, len(steps))

	for len(out) < len(steps) {
		progress := false

		for _, original := range steps {
			s := byID[original.ID]
			if done[s.ID] {
				continue
			}

			ready := true

			for _, dep := range s.DependsOn {
				if !done[dep] {
					ready = false
					break
				}
			}

			if ready {
				out = append(out, s)
				done[s.ID] = true
				progress = true
			}
		}

		if !progress {
			return nil, fmt.Errorf("workflow dependency cycle")
		}
	}

	return out, nil
}
