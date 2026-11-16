package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
)

const maxRecoveries = 32
const maxRecoveryNote = 2000
const maxRecoverySize = 64 << 10

// Recover requires exclusive ownership of an interrupted run. Decisions and the
// prepared checkpoint commit before any resumed effects, using the ordinary executor.
func Recover(ctx context.Context, previous workflow.Run, request workflow.Recovery, h workflow.Handler, save func(workflow.Run, ...int) error) (workflow.Run, error) {
	if previous.Status != "running" {
		return previous, fmt.Errorf("only interrupted execution can be recovered")
	}

	encodedRequest, err := json.Marshal(request)
	if err != nil || len(encodedRequest) > maxRecoverySize {
		return previous, fmt.Errorf("recovery decisions exceed 64 KiB or contain invalid JSON")
	}
	if strings.TrimSpace(request.Note) == "" || len(request.Note) > maxRecoveryNote || len(previous.Recoveries) >= maxRecoveries {
		return previous, fmt.Errorf("recovery requires a note of 1–%d bytes and at most %d recoveries per run", maxRecoveryNote, maxRecoveries)
	}

	definition, err := json.Marshal(previous.Definition)
	if err != nil {
		return previous, err
	}
	digest := sha256.Sum256(definition)
	if fmt.Sprintf("%x", digest) != previous.Digest {
		return previous, fmt.Errorf("run checkpoint does not match definition")
	}
	_, _, err = recipe.Prepare(previous.Definition, previous.Inputs, h)
	if err != nil {
		return previous, err
	}

	uncertain := workflow.UncertainSteps(previous)
	if len(request.Decisions) != len(uncertain) {
		return previous, fmt.Errorf("recovery requires one decision for each of %d uncertain invocations", len(uncertain))
	}
	decisions := make(map[string]workflow.RecoveryDecision, len(uncertain))
	for _, decision := range request.Decisions {
		_, duplicate := decisions[decision.StepID]
		valid := decision.Action == "retry" && len(decision.Output) == 0
		valid = valid || (decision.Action == "complete" && len(decision.Output) <= recipe.MaxOutput && json.Valid(decision.Output))
		if duplicate || !valid {
			return previous, fmt.Errorf("invalid or duplicate recovery decision for %q", decision.StepID)
		}
		decisions[decision.StepID] = decision
	}

	record := workflow.RecoveryRecord{At: time.Now().UTC(), Note: request.Note}
	for _, step := range uncertain {
		decision, ok := decisions[step.ID]
		if !ok {
			return previous, fmt.Errorf("missing recovery decision for %q", step.ID)
		}
		resolution := workflow.RecoveryResolution{StepID: step.ID, Action: decision.Action}
		if decision.Action == "complete" {
			digest := sha256.Sum256(decision.Output)
			resolution.OutputSHA256 = fmt.Sprintf("%x", digest)
		}
		record.Decisions = append(record.Decisions, resolution)
	}

	// Validation failure must not mutate the caller's checkpoint or its audit history.
	raw, err := json.Marshal(previous)
	if err != nil {
		return previous, err
	}
	var recovered workflow.Run
	err = json.Unmarshal(raw, &recovered)
	if err != nil {
		return previous, err
	}
	err = resolveInterrupted(recovered.Steps, "", decisions)
	if err != nil {
		return previous, err
	}
	recovered.Recoveries = append(recovered.Recoveries, record)
	err = ctx.Err()
	if err != nil {
		return previous, err
	}
	err = save(recovered)
	if err != nil {
		return previous, fmt.Errorf("save recovery decisions: %w", err)
	}

	remaining := workflow.MaxInvocations - invocationCount(recovered)
	scope := executionScope{remaining: &remaining, approved: recovered.Approved}
	return run(ctx, recovered.Definition, recovered.Inputs, h, save, &recovered, scope)
}

func resolveInterrupted(steps []workflow.StepResult, prefix string, decisions map[string]workflow.RecoveryDecision) error {
	for i := range steps {
		step := &steps[i]
		if step.Status == "completed" || step.Status == "skipped" || (step.Status == "failed" && step.Failure != nil) {
			continue
		}
		if step.Status == "failed" || step.Status == "cancelled" {
			return fmt.Errorf("cannot recover unfinished scope containing %s invocation %q", step.Status, prefix+step.ID)
		}

		decision, ok := decisions[prefix+step.ID]
		if ok {
			step.Status, step.Error = "pending", ""
			if decision.Action == "complete" {
				step.Status, step.Output = "completed", string(decision.Output)
			}
		} else if step.Status == "running" {
			step.Status = "pending"
			if step.Wait != nil || step.Callback != nil {
				step.Status = "waiting"
			}
		}

		if step.Child != nil {
			err := resolveInterrupted(step.Child.Steps, prefix+step.ID+"/", decisions)
			if err != nil {
				return err
			}
		}
		// Item IDs already include their enclosing scope and collection position.
		err := resolveInterrupted(step.Iterations, "", decisions)
		if err != nil {
			return err
		}
	}
	return nil
}
