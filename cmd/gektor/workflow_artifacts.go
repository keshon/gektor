package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func workflowArtifacts(ctx context.Context, args []string, out io.Writer, directory string) error {
	command := args[0]
	f := flag.NewFlagSet("workflow "+command, flag.ContinueOnError)
	dir := f.String("workspace", ".", "workspace owning these artifacts")
	var age time.Duration
	pruning := command == "prune-artifacts" || command == "prune-runs"
	if pruning {
		f.DurationVar(&age, "older-than", 0, "remove terminal run data finished before this age; required and positive")
	}
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	counts := map[string]int{"artifacts": 1, "export-artifact": 3, "prune-artifacts": 0, "prune-runs": 0}
	if f.NArg() != counts[command] {
		return fmt.Errorf("workflow %s expects %d arguments; artifacts: run-id; export-artifact: run-id artifact-id destination; flags precede arguments", command, counts[command])
	}
	if pruning && age <= 0 {
		return fmt.Errorf("%s requires a positive -older-than duration, for example 168h", command)
	}
	ws, err := workspace.New(*dir)
	if err != nil {
		return err
	}
	defer ws.Close()
	db, err := store.OpenStore(directory, ws.Root())
	if err != nil {
		return err
	}
	defer db.Close()
	if pruning {
		before := time.Now().Add(-age)
		var removed []string
		if command == "prune-runs" {
			removed, err = db.PruneRuns(ctx, before)
		} else {
			removed, err = db.PruneArtifacts(ctx, before)
		}
		if err != nil {
			return err
		}
		result := struct {
			PrunedRuns []string `json:"pruned_runs"`
		}{PrunedRuns: removed}
		return json.NewEncoder(out).Encode(result)
	}
	w, _, err := db.EditRun(f.Arg(0))
	if err != nil {
		return err
	}
	defer w.Close()
	if command == "artifacts" {
		items, err := w.ListArtifacts()
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(items)
	}
	meta, data, err := w.Artifacts("").Read(ctx, f.Arg(1))
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// The operator supplies an explicit workspace destination. Never overwrite.
	dest := f.Arg(2)
	file, err := ws.CreateExclusive(dest, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("export failed; destination %q may be incomplete: %w", dest, err)
	}
	return json.NewEncoder(out).Encode(map[string]any{"artifact": meta, "destination": dest})
}
