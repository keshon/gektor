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

	resume := func(ctx context.Context) (string, error) {
		runErr := ctx.Err()
		var persistence error
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
			persistence = writer.Save(r)
		} else {
			h := &handler{registry: s.Registry, authorize: s.authorizeNode, authorized: map[authorization]bool{}, templates: true}
			h.artifacts = writer.Artifacts
			save := func(record workflow.Run, path ...int) error {
				saveErr := writer.Save(record, path...)
				if saveErr != nil {
					persistence = saveErr
				}
				return saveErr
			}
			r, runErr = engine.Resume(ctx, r, h, save)
		}

		if persistence != nil {
			return "", &jobPersistenceError{errors.Join(runErr, persistence)}
		}
		out, encodeErr := json.Marshal(struct {
			ID       string          `json:"run_id"`
			Status   string          `json:"status"`
			ResumeAt *time.Time      `json:"resume_at,omitempty"`
			Outputs  json.RawMessage `json:"outputs,omitempty"`
		}{ID: r.RunID, Status: r.Status, ResumeAt: r.WakeAt, Outputs: DeclaredOutputs(r)})
		return string(out), errors.Join(runErr, encodeErr)
	}
	if linked != nil {
		_, err = withJob(ctx, db, *linked, resume)
	} else {
		_, err = resume(ctx)
	}
	// Terminal operation failures stay on the run/job. Broken checkpoints stop the worker.
	var persistence *jobPersistenceError
	if errors.As(err, &persistence) {
		return err
	}
	return nil
}
