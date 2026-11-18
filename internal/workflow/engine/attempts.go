package engine

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func executeAttempts(ctx context.Context, s workflow.Step, h workflow.Handler, r *workflow.Run, index int, save func(workflow.Run, ...int) error) (string, error) {
	limit, delay := 1, 0
	if s.Retry != nil {
		limit = s.Retry.MaxAttempts
		delay = s.Retry.DelayMS
	}
	var out string
	var err error

	for number := 1; number <= limit; number++ {
		err = ctx.Err()
		if err != nil {
			return out, err
		}

		step := &r.Steps[index]
		step.Attempts = append(step.Attempts, workflow.Attempt{Number: len(step.Attempts) + 1, Started: time.Now().UTC()})
		err = save(*r, index)
		if err != nil {
			return out, fmt.Errorf("save attempt: %w", err)
		}

		err = ctx.Err()
		if err == nil {
			out, err = h.Run(ctx, s)
		}

		now := time.Now().UTC()
		attempt := &step.Attempts[len(step.Attempts)-1]
		attempt.Finished = &now
		if err != nil {
			attempt.Error = err.Error()
		}
		attempt.Failure = workflow.OperationFailure(err)
		saveErr := save(*r, index)
		if saveErr != nil {
			return out, fmt.Errorf("save attempt outcome: %w", saveErr)
		}

		contextErr := ctx.Err()
		if contextErr != nil {
			return out, contextErr
		}

		failure := attempt.Failure
		if err == nil || number == limit || failure == nil {
			return out, err
		}
		if len(s.Retry.Codes) > 0 && !slices.Contains(s.Retry.Codes, failure.Code) {
			return out, err
		}

		timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return out, ctx.Err()
		case <-timer.C:
		}
	}

	return out, err
}
