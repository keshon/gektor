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

// Recover is an operator action, deliberately absent from Agent and HTTP adapters.
func (s Service) Recover(ctx context.Context, id string, request workflow.Recovery) (string, error) {
	db, err := store.OpenStore(s.StoreDir, s.WS.Root())
	if err != nil {
		return "", err
	}
	defer db.Close()

	writer, r, err := db.EditRun(id)
	if err != nil {
		return "", err
	}
	defer writer.Close()

	if r.Status != "running" {
		return "", fmt.Errorf("run is not interrupted")
	}
	digest := sha256.Sum256([]byte(r.RecipeSource))
	if fmt.Sprintf("%x", digest) != r.RecipeSHA256 {
		return "", fmt.Errorf("recipe snapshot hash mismatch")
	}

	if s.AuthorizeResource == nil {
		return "", fmt.Errorf("recovery authorization unavailable")
	}
	args, err := json.Marshal(struct {
		Path string `json:"recipe_path"`
	}{Path: r.RecipePath})
	if err != nil {
		return "", err
	}
	err = s.AuthorizeResource(ctx, "workflow_run", r.RecipePath, args)
	if err != nil {
		return "", err
	}
	for _, step := range workflow.ScopedSteps(r) {
		if step.Result.Callback != nil && (step.Result.Status == "running" || step.Result.Status == "waiting") {
			err = s.authorizeNode(ctx, step.Definition.Tool, step.Result.Args)
			if err != nil {
				return "", err
			}
		}
	}

	candidates := map[string]workflow.ScopedStep{}
	for _, step := range workflow.UncertainSteps(r) {
		candidates[step.ID] = step
	}
	for _, decision := range request.Decisions {
		step, ok := candidates[decision.StepID]
		if !ok {
			return "", fmt.Errorf("invocation %q is not uncertain", decision.StepID)
		}
		if decision.Action != "complete" {
			continue
		}
		node, nodeErr := s.Registry.Node(step.Definition.Tool)
		if nodeErr != nil {
			return "", nodeErr
		}
		if len(node.OutputSchema) > 0 {
			err = workflow.ValidateJSON(decision.Output, node.OutputSchema)
			if err != nil {
				return "", fmt.Errorf("recovery output for %s: %w", step.ID, err)
			}
		}
		err = s.authorizeNode(ctx, step.Definition.Tool, step.Result.Args)
		if err != nil {
			return "", err
		}
	}

	jobs, err := db.Jobs(ctx)
	if err != nil {
		return "", err
	}
	var linked *store.Job
	for _, job := range jobs {
		if job.RunID == id {
			if job.Cancel {
				return "", fmt.Errorf("job has a pending cancellation; recovery denied")
			}
			copy := job
			linked = &copy
			break
		}
	}

	execute := func(runctx context.Context) (string, error) {
		h := &handler{
			registry:   s.Registry,
			authorize:  s.authorizeNode,
			authorized: map[authorization]bool{},
			templates:  true,
			artifacts:  writer.Artifacts,
		}
		save := func(record workflow.Run, path ...int) error {
			err := writer.Save(record, path...)
			if err != nil {
				return &persistenceError{err}
			}
			return nil
		}
		var runErr error
		r, runErr = engine.Recover(runctx, r, request, h, save)
		status := r.Status
		if status == "running" {
			status = "interrupted"
		}
		encoded, encodeErr := json.Marshal(struct {
			ID       string                  `json:"run_id"`
			Status   string                  `json:"status"`
			Outputs  json.RawMessage         `json:"outputs,omitempty"`
			Callback *workflow.CallbackState `json:"callback,omitempty"`
			ResumeAt *time.Time              `json:"resume_at,omitempty"`
		}{ID: id, Status: status, Outputs: DeclaredOutputs(r), Callback: workflow.PendingCallback(r), ResumeAt: r.WakeAt})
		return string(encoded), errors.Join(runErr, encodeErr)
	}
	if linked != nil {
		// A failed queue checkpoint may have left a terminal job beside an
		// interrupted run. The exclusively leased run is authoritative here.
		err = db.UpdateJob(ctx, linked.ID, func(job *store.Job) error {
			if job.RunID != id || job.Cancel {
				return fmt.Errorf("linked job changed or was cancelled")
			}
			job.Status = "interrupted"
			return nil
		})
		if err != nil {
			return "", err
		}
		return withJob(ctx, db, *linked, execute)
	}
	return execute(ctx)
}
