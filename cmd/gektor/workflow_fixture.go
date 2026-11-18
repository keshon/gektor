package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workflow/fixture"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func workflowCapture(ctx context.Context, args []string, out io.Writer, storeDir string) error {
	flags := flag.NewFlagSet("workflow capture", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String("workspace", ".", "workspace whose completed run to capture")

	err := flags.Parse(args)
	if err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: gektor workflow capture [-workspace dir] <run-id>")
	}

	err = ctx.Err()
	if err != nil {
		return err
	}

	db, err := store.OpenStore(storeDir, *directory)
	if err != nil {
		return err
	}
	defer db.Close()
	state, err := db.Run(flags.Arg(0))
	if err != nil {
		return err
	}

	raw, err := fixture.Capture(state.Record, service.DeclaredOutputs(state.Record))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(raw))
	return err
}

func workflowFixtureTest(ctx context.Context, args []string, out io.Writer, storeDir string, gate permission.Gate) error {
	flags := flag.NewFlagSet("workflow test", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String("workspace", ".", "workspace containing recipe and fixture files")
	path := flags.String("fixture", "", "workspace-relative fixture JSON file")
	allow := flags.String("allow", "", "additional tool=pattern read permission rules")
	deny := flags.String("deny", "", "denied tool=pattern read permission rules")
	yes := flags.Bool("yes", false, "deny permission prompts in unattended tests")
	timeout := flags.Duration("run-timeout", 5*time.Minute, "fixture test deadline; 0 disables")

	err := flags.Parse(args)
	if err != nil {
		return err
	}
	if flags.NArg() != 1 || *path == "" || *timeout < 0 {
		return fmt.Errorf("usage: gektor workflow test -fixture <fixture.json> [flags] <recipe.json>")
	}

	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	err = ctx.Err()
	if err != nil {
		return err
	}

	policy := buildPolicy(*allow, *deny)
	err = policy.Validate()
	if err != nil {
		return err
	}

	ws, err := workspace.New(*directory)
	if err != nil {
		return err
	}
	defer ws.Close()
	if *yes {
		gate = nil
	}
	svc, err := app.NewWorkflow(ws, storeDir, policy, gate)
	if err != nil {
		return err
	}

	request, err := json.Marshal(struct {
		Path string `json:"path"`
	}{Path: *path})
	if err != nil {
		return err
	}
	err = svc.Authorize(ctx, "read_file", request)
	if err != nil {
		return err
	}

	file, err := ws.Open(*path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("fixture must be a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, fixture.MaxBytes+1))
	if err != nil {
		return err
	}

	report, testErr := svc.Test(ctx, flags.Arg(0), raw)
	writeErr := json.NewEncoder(out).Encode(report)
	return errors.Join(testErr, writeErr)
}
