package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/engine"
	"github.com/keshon/gektor/internal/workflow/store"
)

// Review is an operator action. Agent adapters deliberately do not expose it.
func (s Service) Review(ctx context.Context, id, step string, approve bool) (Result, error) {
	db, err := store.OpenStore(s.StoreDir, s.WS.Root())
	if err != nil {
		return Result{}, err
	}
	defer db.Close()
	w, r, err := db.EditRun(id)
	if err != nil {
		return Result{}, err
	}
	defer w.Close()
	if r.Status != "waiting_review" {
		return Result{}, fmt.Errorf("run is not waiting for review")
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(r.RecipeSource))) != r.RecipeSHA256 {
		return Result{}, fmt.Errorf("recipe snapshot hash mismatch")
	}
	found := false
	for _, v := range workflow.ScopedSteps(r) {
		if v.ID == step && v.Result.Status == "waiting_review" && v.Definition.Review && !r.Approved[v.ID] {
			found = true
		}
	}
	if !found {
		return Result{}, fmt.Errorf("step is not waiting for review")
	}
	if s.AuthorizeResource == nil {
		return Result{}, fmt.Errorf("review authorization unavailable")
	}
	args, _ := json.Marshal(map[string]string{"recipe_path": r.RecipePath})
	if err := s.AuthorizeResource(ctx, "workflow_run", r.RecipePath, args); err != nil {
		return Result{}, err
	}

	jobs, err := db.Jobs(ctx)
	if err != nil {
		return Result{}, err
	}
	var linked *store.Job
	for _, job := range jobs {
		if job.RunID == id {
			copy := job
			linked = &copy
			break
		}
	}
	execute := func(ctx context.Context) (Result, error) {
		save := func(record workflow.Run, path ...int) error {
			err := w.Save(record, path...)
			if err != nil {
				return &persistenceError{err}
			}
			return nil
		}
		var runErr error
		if approve {
			if r.Approved == nil {
				r.Approved = map[string]bool{}
			}
			r.Approved[step] = true
			h := &handler{registry: s.Registry, authorize: s.authorizeNode, authorized: map[authorization]bool{}, templates: true}
			h.artifacts = w.Artifacts
			r, runErr = engine.Resume(ctx, r, h, save)
		} else {
			now := time.Now().UTC()
			cancelPaused(&r, now)
			runErr = save(r)
		}
		return Result{RunID: id, Status: r.Status, Outputs: DeclaredOutputs(r)}, runErr
	}
	if linked != nil {
		return withJob(ctx, db, *linked, execute)
	}
	return execute(ctx)
}

func cancelPaused(r *workflow.Run, now time.Time) {
	r.Status, r.Finished, r.WakeAt = "cancelled", &now, nil
	cancelPausedSteps(r.Steps, now)
}

func cancelPausedSteps(steps []workflow.StepResult, now time.Time) {
	for i := range steps {
		step := &steps[i]
		if step.Status != "waiting_review" && step.Status != "waiting" && step.Status != "pending" {
			continue
		}
		step.Status = "skipped"
		if step.Child != nil {
			cancelPaused(step.Child, now)
		}
		cancelPausedSteps(step.Iterations, now)
	}
}
