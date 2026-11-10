package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
)

// Serve owns one workspace queue. Policy is supplied by the operator, never jobs.
func Serve(ctx context.Context, s *store.Store, svc service.Service) (resultErr error) {
	lease, err := s.DaemonLease()
	if err != nil {
		return fmt.Errorf("workflow service already active or unavailable: %w", err)
	}
	defer lease.Close()
	if err := s.RecoverJobs(ctx); err != nil {
		return err
	}

	ctx, stop := context.WithCancel(ctx)
	tickDone := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			err := s.ReconcileRuns(ctx)
			if err == nil {
				err = s.Tick(ctx, time.Now())
			}
			if err != nil {
				tickDone <- err
				stop()
				return
			}
			select {
			case <-ctx.Done():
				tickDone <- nil
				return
			case <-ticker.C:
			}
		}
	}()
	defer func() {
		stop()
		if err := <-tickDone; err != nil {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		job, ok, err := s.Claim(ctx)
		if err != nil {
			return err
		}
		if !ok {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}
		if err := svc.ExecuteJob(ctx, s, job); err != nil {
			return err
		}
	}
}
