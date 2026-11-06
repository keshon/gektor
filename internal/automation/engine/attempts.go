package engine

import (
	"context"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	"time"
)

func executeAttempts(ctx context.Context, s automation.Step, h automation.Handler, r *automation.Run, index int, save func(automation.Run) error) (string, error) {
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
		step.Attempts = append(step.Attempts, automation.Attempt{Number: number, Started: time.Now().UTC()})
		if err := save(*r); err != nil {
			return out, fmt.Errorf("save attempt: %w", err)
		}
		out, err = h.Run(ctx, s)
		now := time.Now().UTC()
		attempt := &step.Attempts[len(step.Attempts)-1]
		attempt.Finished = &now
		if err != nil {
			attempt.Error = err.Error()
		}
		if e := save(*r); e != nil {
			return out, fmt.Errorf("save attempt outcome: %w", e)
		}
		if err == nil || number == limit {
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
