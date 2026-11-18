package workflow

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// StepError carries a node location without changing the underlying error identity.
// Pointer is relative to the step; an empty pointer identifies the whole step.
type StepError struct {
	StepID  string
	Pointer string
	Err     error
}

func (e *StepError) Error() string { return fmt.Sprintf("step %s: %v", e.StepID, e.Err) }
func (e *StepError) Unwrap() error { return e.Err }

type Diagnostic struct {
	StepID  string `json:"step_id,omitempty"`
	Pointer string `json:"pointer,omitempty"`
	Message string `json:"message"`
}

// Diagnose reports locations for navigation, never instructions to edit a recipe.
// Schema pointers are recipe-relative unless a StepError supplies node context.
// Older runtime errors retain best-effort step matching; unknown locations stay blank.
func Diagnose(err error, definition Definition) []Diagnostic {
	if err == nil {
		return nil
	}
	var step *StepError
	errors.As(err, &step)
	id, prefix := "", ""
	if step != nil {
		id, prefix = step.StepID, step.Pointer
	} else {
		for _, candidate := range definition.Steps {
			for _, suffix := range []string{":", " ", "/"} {
				if strings.Contains(err.Error(), "step "+candidate.ID+suffix) && len(candidate.ID) > len(id) {
					id = candidate.ID
				}
			}
		}
	}

	var schema *jsonschema.ValidationError
	if !errors.As(err, &schema) {
		return []Diagnostic{{StepID: id, Pointer: prefix, Message: err.Error()}}
	}
	diagnostics := []Diagnostic{}
	var visit func(*jsonschema.ValidationError)
	visit = func(failure *jsonschema.ValidationError) {
		if len(failure.Causes) > 0 {
			for _, cause := range failure.Causes {
				visit(cause)
			}
			return
		}
		location := failure.InstanceLocation
		node := id
		if node == "" && len(location) >= 2 && location[0] == "steps" {
			index, indexErr := strconv.Atoi(location[1])
			if indexErr == nil && index >= 0 && index < len(definition.Steps) {
				node = definition.Steps[index].ID
				location = location[2:]
			}
		}
		pointer := prefix
		for _, part := range location {
			pointer += "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(part)
		}
		diagnostics = append(diagnostics, Diagnostic{StepID: node, Pointer: pointer, Message: failure.Error()})
	}
	visit(schema)
	return diagnostics
}
