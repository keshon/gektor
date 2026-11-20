package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/engine"
	"github.com/keshon/gektor/internal/workflow/store"
)

// ContinueWait borrows an exclusively leased writer. The worker closes it after
// this call joins execution and any queue cancellation monitor.
func (s Service) ContinueWait(ctx context.Context, db *store.Store, writer *store.RunWriter, r workflow.Run) error {
	if r.Status != "waiting" || r.WakeAt == nil {
		return fmt.Errorf("run has no pending wait")
	}
	jobs, err := db.Jobs(ctx)
	if err != nil {
		return err
	}
	var linked *store.Job
	for _, job := range jobs {
		if job.RunID == r.RunID {
			linkedJob := job
			linked = &linkedJob
			break
		}
	}

	resume := func(ctx context.Context) (Result, error) {
		save := func(record workflow.Run, path ...int) error {
			err := writer.Save(record, path...)
			if err != nil {
				return &persistenceError{err}
			}
			return nil
		}
		runErr := ctx.Err()
		if runErr == nil {
			args, encodeErr := json.Marshal(struct {
				Path string `json:"recipe_path"`
			}{Path: r.RecipePath})
			runErr = encodeErr
			digest := sha256.Sum256([]byte(r.RecipeSource))
			actualDigest := fmt.Sprintf("%x", digest)
			if runErr == nil && actualDigest != r.RecipeSHA256 {
				runErr = fmt.Errorf("recipe snapshot hash mismatch")
			}
			if runErr == nil && s.AuthorizeResource == nil {
				runErr = fmt.Errorf("wait continuation authorization unavailable")
			}
			if runErr == nil {
				runErr = s.AuthorizeResource(ctx, "workflow_run", r.RecipePath, args)
			}
			if runErr == nil {
				for _, step := range workflow.ScopedSteps(r) {
					if step.Result.Status == "waiting" && step.Result.Callback != nil {
						runErr = s.authorizeNode(ctx, step.Definition.Tool, step.Result.Args)
						break
					}
				}
			}
		}

		if runErr != nil {
			boundary := -1
			for i, step := range r.Steps {
				if step.Status == "waiting" {
					boundary = i
					break
				}
			}
			cancelPaused(&r, time.Now().UTC())
			if ctx.Err() == nil {
				r.Status = "failed"
				if boundary >= 0 {
					r.Steps[boundary].Status, r.Steps[boundary].Error = "failed", runErr.Error()
				}
			}
			saveErr := save(r)
			if saveErr != nil {
				return Result{}, errors.Join(runErr, saveErr)
			}
		} else {
			h := &handler{registry: s.Registry, authorize: s.authorizeNode, authorized: map[authorization]bool{}, templates: true}
			h.artifacts = writer.Artifacts
			r, runErr = engine.Resume(ctx, r, h, save)
		}
		return Result{
			RunID:    r.RunID,
			Status:   r.Status,
			ResumeAt: r.WakeAt,
			Callback: workflow.PendingCallback(r),
			Outputs:  DeclaredOutputs(r),
		}, runErr
	}
	if linked != nil {
		_, err = withJob(ctx, db, *linked, resume)
	} else {
		_, err = resume(ctx)
	}
	// Terminal operation failures stay on the run/job. Broken checkpoints stop the worker.
	var persistence *persistenceError
	if errors.As(err, &persistence) {
		return err
	}
	return nil
}
