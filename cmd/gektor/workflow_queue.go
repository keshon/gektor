package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/automation/daemon"
	"github.com/keshon/gektor/internal/automation/store"
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
	overlap := flags.String("overlap", "skip", "active schedule: skip or queue")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	counts := map[string]int{"review": 2, "serve": 0, "submit": 1, "jobs": 0, "job": 1, "cancel": 1, "schedules": 0, "schedule": 2, "pause": 1, "resume": 1, "unschedule": 1}
	count, ok := counts[command]
	if !ok || flags.NArg() != count {
		return fmt.Errorf("workflow %s expects %d arguments (flags precede arguments)", command, count)
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
		jobs, err := s.Jobs(ctx)
		if err != nil {
			return err
		}
		for _, j := range jobs {
			if j.ID == flags.Arg(0) {
				return emit(j, nil)
			}
		}
		return fmt.Errorf("unknown job %s", flags.Arg(0))
	case "cancel":
		return s.CancelJob(ctx, flags.Arg(0))
	case "schedules":
		return emit(s.Schedules(ctx))
	case "pause", "resume":
		return s.SetSchedulePaused(ctx, flags.Arg(0), command == "pause")
	case "unschedule":
		return s.DeleteSchedule(ctx, flags.Arg(0))
	}
	// Unattended execution never prompts and never treats missing rules as approval.
	svc, err := app.NewAutomation(ws, directory, policy, permissionGateContext(true, nil, nil))
	if err != nil {
		return err
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
		return daemon.Serve(ctx, s, svc)
	}
	path := flags.Arg(0)
	if command == "schedule" {
		path = flags.Arg(1)
	}
	path, err = ws.Relative(path)
	if err != nil {
		return err
	}
	source, err := svc.Snapshot(ctx, path)
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
