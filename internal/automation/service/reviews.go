package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation/engine"
	"github.com/keshon/gektor/internal/automation/store"
	"time"
)

// Review is an operator action. Agent adapters deliberately do not expose it.
func (s Service) Review(ctx context.Context, id, step string, approve bool) (string, error) {
	db, err := store.OpenStore(s.StoreDir, s.WS.Root())
	if err != nil {
		return "", err
	}
	defer db.Close()
	w, r, err := db.EditRun(id)
	if err != nil {
		return "", err
	}
	defer w.Close()
	if r.Status != "waiting_review" {
		return "", fmt.Errorf("run is not waiting for review")
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(r.RecipeSource))) != r.RecipeSHA256 {
		return "", fmt.Errorf("recipe snapshot hash mismatch")
	}
	found := false
	for _, v := range r.Steps {
		if v.ID == step && v.Status == "waiting_review" {
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("step is not waiting for review")
	}
	if s.AuthorizeResource == nil {
		return "", fmt.Errorf("review authorization unavailable")
	}
	args, _ := json.Marshal(map[string]string{"recipe_path": r.RecipePath})
	if err := s.AuthorizeResource(ctx, "workflow_run", r.RecipePath, args); err != nil {
		return "", err
	}

	jobs, err := db.Jobs(ctx)
	if err != nil {
		return "", err
	}
	var linked *store.Job
	for _, job := range jobs {
		if job.RunID == id {
			copy := job
			linked = &copy
			break
		}
	}
	execute := func(ctx context.Context) (string, error) {
		var runErr error
		if approve {
			if r.Approved == nil {
				r.Approved = map[string]bool{}
			}
			r.Approved[step] = true
			h := handler{registry: s.Registry, authorize: s.authorizeNode, authorized: map[authorization]bool{}, templates: true}
			h.artifacts = w.Artifacts
			r, runErr = engine.Resume(ctx, r, h, w.Save)
		} else {
			r.Status = "cancelled"
			now := time.Now().UTC()
			r.Finished = &now
			for i := range r.Steps {
				if r.Steps[i].Status == "waiting_review" || r.Steps[i].Status == "pending" {
					r.Steps[i].Status = "skipped"
				}
			}
			runErr = w.Save(r)
		}
		summary := map[string]any{"run_id": id, "status": r.Status}
		if outputs := runOutputs(r); outputs != nil {
			summary["outputs"] = outputs
		}
		result, _ := json.Marshal(summary)
		return string(result), runErr
	}
	if linked != nil {
		return withJob(ctx, db, *linked, execute)
	}
	return execute(ctx)
}
