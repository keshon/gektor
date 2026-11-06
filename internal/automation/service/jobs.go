package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/keshon/gektor/internal/automation/store"
	"time"
)

// ExecuteJob shares cancellation and final persistence with operator continuation.
func (s Service) ExecuteJob(ctx context.Context, db *store.Store, job store.Job) error {
	current := s
	current.OnRun = func(id string) error {
		return db.UpdateJob(ctx, job.ID, func(j *store.Job) error { j.RunID = id; return nil })
	}
	_, err := withJob(ctx, db, job, func(ctx context.Context) (string, error) {
		if current.AuthorizeResource == nil {
			return "", fmt.Errorf("queue authorization unavailable")
		}
		args, _ := json.Marshal(map[string]string{"recipe_path": job.RecipePath})
		if err := current.AuthorizeResource(ctx, "workflow_run", job.RecipePath, args); err != nil {
			return "", err
		}
		return current.RunSource(ctx, job.Source, job.RecipePath, job.Inputs)
	})
	// Operation failures are recorded on the job; only persistence failures stop the worker.
	var persistence *jobPersistenceError
	if errors.As(err, &persistence) {
		return err
	}
	return nil
}

type jobPersistenceError struct{ error }

func withJob(ctx context.Context, db *store.Store, job store.Job, execute func(context.Context) (string, error)) (string, error) {
	cancelRequested := false
	if err := db.UpdateJob(ctx, job.ID, func(j *store.Job) error {
		if j.Status != "running" && j.Status != "waiting_review" {
			return fmt.Errorf("job is not executable: %s", j.Status)
		}
		cancelRequested = j.Cancel
		j.Status = "running"
		return nil
	}); err != nil {
		return "", &jobPersistenceError{err}
	}
	runctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if cancelRequested {
		cancel()
	}
	done := make(chan struct{})
	polled := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			jobs, err := db.Jobs(runctx)
			if err != nil {
				cancel()
				polled <- err
				return
			}
			for _, j := range jobs {
				if j.ID == job.ID && j.Cancel {
					cancel()
					polled <- nil
					return
				}
			}
			select {
			case <-done:
				polled <- nil
				return
			case <-runctx.Done():
				polled <- nil
				return
			case <-ticker.C:
			}
		}
	}()
	result, runErr := execute(runctx)
	close(done)
	pollErr := <-polled
	persist, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	err := db.UpdateJob(persist, job.ID, func(j *store.Job) error {
		var summary struct{ Status string }
		_ = json.Unmarshal([]byte(result), &summary)
		switch summary.Status {
		case "completed", "waiting_review", "failed", "cancelled":
			j.Status = summary.Status
		default:
			j.Status = "failed"
			if runctx.Err() != nil {
				j.Status = "cancelled"
			}
		}
		j.Error = ""
		if runErr != nil {
			j.Error = runErr.Error()
		}
		return nil
	})
	if err != nil {
		return result, errors.Join(runErr, &jobPersistenceError{err})
	}
	if pollErr != nil && ctx.Err() == nil && !errors.Is(pollErr, context.Canceled) {
		return result, errors.Join(runErr, &jobPersistenceError{pollErr})
	}
	return result, runErr
}
