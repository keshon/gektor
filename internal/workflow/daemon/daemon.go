// Package daemon serves a workspace workflow queue with operator-supplied policy.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
)

const MaxJobs = 32
const defaultMaxOperations = 32

// Config limits work owned by one worker process. Zero values use one job and
// 32 active operations. Service dependencies must support concurrent use.
type Config struct {
	MaxJobs       int
	MaxOperations int
}

// Serve holds the workspace lease until its scheduler and all jobs have stopped.
// Jobs and due continuations share the same slots and operator-supplied policy.
func Serve(ctx context.Context, s *store.Store, svc service.Service, cfg Config) (resultErr error) {
	if cfg.MaxJobs == 0 {
		cfg.MaxJobs = 1
	}
	if cfg.MaxOperations == 0 {
		cfg.MaxOperations = defaultMaxOperations
	}
	if cfg.MaxJobs < 1 || cfg.MaxJobs > MaxJobs {
		return fmt.Errorf("active jobs must be between 1 and %d", MaxJobs)
	}
	registry, err := svc.Registry.Bounded(cfg.MaxOperations)
	if err != nil {
		return err
	}
	svc.Registry = registry

	lease, err := s.DaemonLease()
	if err != nil {
		return fmt.Errorf("workflow service already active or unavailable: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()

	err = s.SaveWorkerTick(ctx, time.Time{})
	if err != nil {
		return err
	}
	err = s.ReconcileArtifacts(ctx)
	if err != nil {
		return err
	}
	err = s.RecoverJobs(ctx)
	if err != nil {
		return err
	}

	workerCtx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, cfg.MaxJobs+1)
	go func() { done <- advanceSchedules(workerCtx, s) }()
	for range cfg.MaxJobs {
		go func() { done <- executeJobs(workerCtx, s, svc) }()
	}

	for range cfg.MaxJobs + 1 {
		err = <-done
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			resultErr = errors.Join(resultErr, err)
		}
		stop()
	}
	// A cancelled claim may not have created its run yet. No worker remains to own it.
	persistCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
	defer finish()
	reconcileErr := s.RecoverJobs(persistCtx)
	return errors.Join(resultErr, ctx.Err(), reconcileErr)
}

func advanceSchedules(ctx context.Context, s *store.Store) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		err := s.ReconcileArtifacts(ctx)
		if err == nil {
			err = s.ReconcileRuns(ctx)
		}
		if err == nil {
			err = s.Tick(ctx, time.Now())
		}
		if err == nil {
			err = s.SaveWorkerTick(ctx, time.Now())
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func executeJobs(ctx context.Context, s *store.Store, svc service.Service) error {
	for {
		err := ctx.Err()
		if err != nil {
			return err
		}
		// Alternate due continuations with queued work so neither backlog starves.
		resumed, err := resumeWait(ctx, s, svc)
		if err != nil {
			return err
		}
		job, claimed, err := s.Claim(ctx)
		if err != nil {
			return err
		}
		if claimed {
			err = svc.ExecuteJob(ctx, s, job)
			if err != nil {
				return err
			}
			continue
		}
		if resumed {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func resumeWait(ctx context.Context, db *store.Store, svc service.Service) (bool, error) {
	writer, r, ok, err := db.ClaimWait(ctx, time.Now())
	if err != nil || !ok {
		return false, err
	}

	err = svc.ContinueWait(ctx, db, writer, r)
	closeErr := writer.Close()
	return true, errors.Join(err, closeErr)
}
