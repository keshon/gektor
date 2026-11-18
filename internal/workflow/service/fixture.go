package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/engine"
	"github.com/keshon/gektor/internal/workflow/fixture"
)

type FixtureReport struct {
	Mode    string          `json:"mode"`
	Passed  bool            `json:"passed"`
	Status  string          `json:"status"`
	Outputs json.RawMessage `json:"outputs,omitempty"`
}

// Test runs in memory with a separate handler. Only the registry's named pure
// builtins execute; all external calls require exact fixture matches.
func (s Service) Test(ctx context.Context, path string, raw []byte) (FixtureReport, error) {
	report := FixtureReport{Mode: "fixture"}
	session, err := fixture.New(raw)
	if err != nil {
		return report, err
	}

	source, err := s.SnapshotRecipe(ctx, path)
	if err != nil {
		return report, err
	}
	definition, _, err := s.Compile(ctx, source, path)
	if err != nil {
		return report, err
	}

	err = fixtureControls(definition)
	if err != nil {
		return report, err
	}

	h := &fixtureHandler{
		validation: &handler{registry: s.Registry, templates: true},
		session:    session,
	}

	// The engine still checkpoints its ordering; this mode has no durable run or jobs.
	checkpoint := func(workflow.Run, ...int) error { return ctx.Err() }
	run, runErr := engine.RunWithInputs(ctx, definition, session.Inputs, h, checkpoint)
	report.Status, report.Outputs = run.Status, DeclaredOutputs(run)
	if runErr != nil && workflow.OperationFailure(runErr) == nil {
		return report, runErr
	}

	err = session.Check(run.Status, report.Outputs)
	if err != nil {
		return report, errors.Join(runErr, err)
	}
	report.Passed = true
	return report, nil
}

func fixtureControls(definition workflow.Definition) error {
	for _, step := range definition.Steps {
		if step.Review || step.Tool == "workflow_wait" || step.Tool == "workflow_wait_callback" {
			return fmt.Errorf("fixture mode does not model reviews or durable waits (step %s)", step.ID)
		}
		if step.Child != nil {
			err := fixtureControls(*step.Child)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

type fixtureHandler struct {
	validation *handler
	session    *fixture.Session
}

func (h *fixtureHandler) Validate(step workflow.Step) error { return h.validation.Validate(step) }

func (h *fixtureHandler) Run(ctx context.Context, step workflow.Step) (string, error) {
	node, err := h.validation.registry.Node(step.Tool)
	if err != nil {
		return "", err
	}

	var output string
	if fixture.Pure(step.Tool) {
		output, err = node.Execute(ctx, step.Args)
	} else {
		output, err = h.session.Run(ctx, step)
	}
	if err != nil {
		return output, err
	}

	if len(node.OutputSchema) != 0 {
		err = workflow.ValidateJSON(json.RawMessage(output), node.OutputSchema)
	}
	return output, err
}
