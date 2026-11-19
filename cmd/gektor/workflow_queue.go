package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/api"
	"github.com/keshon/gektor/internal/workflow/daemon"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func workflowQueue(ctx context.Context, args []string, out io.Writer, directory string) error {
	command := args[0]
	flags := flag.NewFlagSet("workflow "+command, flag.ContinueOnError)
	dir := flags.String("workspace", ".", "workspace owning this queue")
	allow := flags.String("allow", "", "allowed tool=pattern rules for this service")
	deny := flags.String("deny", "", "denied tool=pattern rules")
	inputs := flags.String("inputs", "", "workspace-relative JSON input file")
	cron := flags.String("cron", "", "five-field cron expression")
	zone := flags.String("timezone", "UTC", "IANA time zone")
	missed := flags.String("missed", "skip", "missed triggers: skip or once")
	decision := flags.String("decision", "", "review decision: approve or reject")
	recovery := flags.String("recovery", "", "workspace-relative JSON recovery decisions file (operator only)")
	overlap := flags.String("overlap", "skip", "active schedule: skip or queue")
	listen := flags.String("listen", "", "optional loopback HTTP address, e.g. 127.0.0.1:9841")
	apiConfig := flags.String("api-config", "", "workspace-relative API route and token-environment configuration")
	workerConfig := daemon.Config{
		MaxJobs:       1,
		MaxOperations: workflow.MaxConcurrency,
	}
	if command == "serve" {
		flags.IntVar(&workerConfig.MaxJobs, "max-jobs", 1, "maximum active jobs and wait continuations (1–32)")
		flags.IntVar(&workerConfig.MaxOperations, "max-operations", workflow.MaxConcurrency, "maximum active operations shared by this worker (1–32)")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	counts := map[string]int{"recover": 1, "review": 2, "serve": 0, "submit": 1, "jobs": 0, "job": 1, "cancel": 1, "schedules": 0, "schedule": 2, "pause": 1, "resume": 1, "unschedule": 1}
	count, ok := counts[command]
	if !ok || flags.NArg() != count {
		return fmt.Errorf("workflow %s expects %d arguments (flags precede arguments)", command, count)
	}
	if *recovery != "" && command != "recover" {
		return fmt.Errorf("-recovery requires workflow recover")
	}
	if (*listen != "" || *apiConfig != "") && (command != "serve" || *listen == "" || *apiConfig == "") {
		return fmt.Errorf("-listen and -api-config must be used together with workflow serve")
	}
	validJobs := workerConfig.MaxJobs >= 1 && workerConfig.MaxJobs <= daemon.MaxJobs
	validOperations := workerConfig.MaxOperations >= 1 && workerConfig.MaxOperations <= workflow.MaxConcurrency
	if command == "serve" && (!validJobs || !validOperations) {
		return fmt.Errorf("-max-jobs and -max-operations must be between 1 and 32")
	}
	policy := buildPolicy(*allow, *deny)
	if err := policy.Validate(); err != nil {
		return err
	}
	ws, err := workspace.New(*dir)
	if err != nil {
		return err
	}
	defer ws.Close()
	s, err := store.OpenStore(directory, ws.Root())
	if err != nil {
		return err
	}
	defer s.Close()
	emit := func(value any, err error) error {
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(value)
	}
	switch command {
	case "jobs":
		jobs, err := s.Jobs(ctx)
		if err != nil {
			return err
		}
		summaries := make([]map[string]any, 0, len(jobs))
		for _, j := range jobs {
			summaries = append(summaries, map[string]any{"id": j.ID, "status": j.Status, "run_id": j.RunID, "recipe_path": j.RecipePath, "created": j.Created, "error": j.Error})
		}
		return emit(summaries, nil)
	case "job":
		return emit(s.Job(ctx, flags.Arg(0)))
	case "cancel":
		return s.CancelJob(ctx, flags.Arg(0))
	case "schedules":
		return emit(s.Schedules(ctx))
	case "pause", "resume":
		return s.SetSchedulePaused(ctx, flags.Arg(0), command == "pause")
	case "unschedule":
		return s.DeleteSchedule(ctx, flags.Arg(0))
	}
	// Unattended execution rejects prompts. The operator's policy still determines
	// default access; -allow does not turn it into an allowlist.
	svc, err := app.NewWorkflow(ws, directory, policy, permissionGateContext(true, nil, nil))
	if err != nil {
		return err
	}
	if command == "recover" {
		if *recovery == "" {
			return fmt.Errorf("recover requires -recovery decisions.json")
		}
		data, readErr := svc.Snapshot(ctx, *recovery)
		if readErr != nil {
			return readErr
		}

		var request workflow.Recovery
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&request)
		if err != nil {
			return err
		}
		err = decoder.Decode(new(any))
		if err != io.EOF {
			return fmt.Errorf("recovery file must contain one JSON object")
		}

		result, recoverErr := svc.Recover(ctx, flags.Arg(0), request)
		if result != "" {
			_, writeErr := fmt.Fprintln(out, result)
			if writeErr != nil {
				return errors.Join(recoverErr, writeErr)
			}
			workflowGuidance(os.Stderr, result, directory, *dir, *allow, *deny)
		}
		return recoverErr
	}
	if command == "review" {
		if *decision != "approve" && *decision != "reject" {
			return fmt.Errorf("review requires -decision approve|reject")
		}
		result, err := svc.Review(ctx, flags.Arg(0), flags.Arg(1), *decision == "approve")
		if result != "" {
			if _, writeErr := fmt.Fprintln(out, result); writeErr != nil {
				return errors.Join(err, writeErr)
			}
			workflowGuidance(os.Stderr, result, directory, *dir, *allow, *deny)
		}
		return err
	}
	if command == "serve" {
		if *listen != "" {
			data, err := svc.Snapshot(ctx, *apiConfig)
			if err != nil {
				return err
			}
			var cfg api.Config
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&cfg); err != nil {
				return err
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				return fmt.Errorf("API configuration must contain one JSON object")
			}
			handler, err := api.New(ctx, s, svc, cfg, nil)
			if err != nil {
				return err
			}
			listener, err := api.Listen(*listen)
			if err != nil {
				return err
			}
			defer listener.Close()
			fmt.Fprintf(os.Stderr, "workflow API listening on http://%s\n", listener.Addr())
			worker := func(workerCtx context.Context) error {
				return daemon.Serve(workerCtx, s, svc, workerConfig)
			}
			return api.Serve(ctx, listener, handler, worker)
		}
		return daemon.Serve(ctx, s, svc, workerConfig)
	}
	path := flags.Arg(0)
	if command == "schedule" {
		path = flags.Arg(1)
	}
	path, err = ws.Relative(path)
	if err != nil {
		return err
	}
	source, err := svc.SnapshotRecipe(ctx, path)
	if err != nil {
		return err
	}
	var values map[string]json.RawMessage
	if *inputs != "" {
		data, err := svc.Snapshot(ctx, *inputs)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		if values == nil {
			return fmt.Errorf("inputs must be a JSON object")
		}
	}
	svc.ValidateOnly = true
	if _, err := svc.RunSource(ctx, source, path, values); err != nil {
		return err
	}
	if command == "submit" {
		return emit(s.Enqueue(ctx, path, source, values))
	}
	if *cron == "" {
		return fmt.Errorf("schedule requires -cron")
	}
	if err := s.PutSchedule(ctx, store.Schedule{ID: flags.Arg(0), RecipePath: path, Source: source, Inputs: values, Cron: *cron, Timezone: *zone, Missed: *missed, Overlap: *overlap}); err != nil {
		return err
	}
	return emit(map[string]string{"schedule": flags.Arg(0)}, nil)
}
