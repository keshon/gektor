package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func executeAttempts(ctx context.Context, s workflow.Step, h workflow.Handler, r *workflow.Run, index int, save func(workflow.Run) error) (string, error) {
	limit, delay := 1, 0
	if s.Retry != nil {
		limit = s.Retry.MaxAttempts
		delay = s.Retry.DelayMS
	}
	var out string
	var err error
	for number := 1; number <= limit; number++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		step := &r.Steps[index]
		step.Attempts = append(step.Attempts, workflow.Attempt{Number: number, Started: time.Now().UTC()})
		if err := save(*r); err != nil {
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
		if e := save(*r); e != nil {
			return out, fmt.Errorf("save attempt outcome: %w", e)
		}
		var execution *workflow.ExecutionError
		if err == nil || number == limit || !errors.As(err, &execution) || ctx.Err() != nil {
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
