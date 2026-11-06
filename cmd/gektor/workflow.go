package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/tools/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

const workflowUsage = "usage: gektor workflow schema | draft <recipe.json> [agent flags] <task> | validate|run [flags] <recipe.json> | runs [-workspace dir] | inspect [-workspace dir] <run-id> | register-schema [-workspace dir] <kind> <schema.json>"

func runWorkflow(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", workflowUsage)
	}
	if args[0] == "draft" {
		o, err := workflowDraftOptions(args[1:])
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		if err != nil {
			return err
		}
		return runConfigured(launchRequest{options: &o, preferencePath: gektor.Sub("application.json")})
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if args[0] == "register-schema" {
		return workflowRegisterSchema(ctx, args[1:], os.Stdout, gektor.Sub("workflows"))
	}
	if args[0] == "runs" || args[0] == "inspect" {
		return workflowHistory(ctx, args, os.Stdout, gektor.Sub("workflows"))
	}
	if args[0] == "schema" {
		if len(args) != 1 {
			return fmt.Errorf("%s", workflowUsage)
		}
		out, err := (workflow.Schema{}).Run(ctx, json.RawMessage(`{}`))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, out)
		return err
	}
	o, err := parseWorkflowOptions(args, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	gate := permissionGateContext(o.yes, stdioSuspender(os.Stderr, nil), nil)
	return executeWorkflow(ctx, o, os.Stdout, gate)
}

func workflowRegisterSchema(ctx context.Context, args []string, out io.Writer, storeDir string) error {
	flags := flag.NewFlagSet("workflow register-schema", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("workspace", ".", "workspace owning this record kind")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("usage: gektor workflow register-schema [-workspace dir] <kind> <schema.json>")
	}
	ws, err := workspace.New(*dir)
	if err != nil {
		return err
	}
	defer ws.Close()
	f, err := ws.Open(flags.Arg(1))
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err := errors.Join(readErr, f.Close()); err != nil {
		return err
	}
	s, err := automation.OpenStore(storeDir, ws.Root())
	if err != nil {
		return err
	}
	defer s.Close()
	digest, err := s.RegisterSchema(ctx, flags.Arg(0), data)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(map[string]string{"kind": flags.Arg(0), "schema_sha256": digest})
}

func workflowHistory(ctx context.Context, args []string, out io.Writer, storeDir string) error {
	flags := flag.NewFlagSet("workflow "+args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("workspace", ".", "workspace whose runs to inspect")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if args[0] == "runs" && flags.NArg() != 0 || args[0] == "inspect" && flags.NArg() != 1 {
		return fmt.Errorf("%s", workflowUsage)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := automation.OpenStore(storeDir, *dir)
	if err != nil {
		return err
	}
	defer s.Close()
	var value any
	if args[0] == "runs" {
		value, err = s.Runs()
	} else {
		value, err = s.Run(flags.Arg(0))
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(value)
}

func workflowDraftOptions(args []string) (launchOptions, error) {
	if len(args) < 2 || args[0] == "" {
		return launchOptions{}, fmt.Errorf("%s", workflowUsage)
	}
	o, err := parseLaunchOptions(args[1:])
	if err != nil {
		return o, err
	}
	for _, name := range []string{"tui", "serve", "resume", "fork", "answer", "plan", "verify-cmd", "images", "mcp", "browser", "browser-profile", "browser-headless", "capabilities", "check-profile", "context-report"} {
		if o.explicitFlags[name] {
			return o, fmt.Errorf("workflow draft does not support -%s", name)
		}
	}
	if o.task == "" {
		return o, fmt.Errorf("workflow draft requires a task")
	}
	o.workflowRecipe = args[0]
	return o, nil
}

type workflowOptions struct {
	workspace, recipe, inputs, allow, deny string
	validateOnly, yes                      bool
	timeout                                time.Duration
	storeDir                               string
}

func parseWorkflowOptions(args []string, note io.Writer) (workflowOptions, error) {
	var o workflowOptions
	if len(args) == 0 || (args[0] != "validate" && args[0] != "run") {
		return o, fmt.Errorf("%s", workflowUsage)
	}
	o.validateOnly = args[0] == "validate"
	flags := flag.NewFlagSet("gektor workflow "+args[0], flag.ContinueOnError)
	flags.SetOutput(note)
	flags.StringVar(&o.workspace, "workspace", ".", "workspace containing recipe and input files")
	flags.StringVar(&o.inputs, "inputs", "", "workspace-relative JSON object file containing version-2 inputs")
	flags.StringVar(&o.allow, "allow", "", "additional tool=pattern permission rules")
	flags.StringVar(&o.deny, "deny", "", "denied tool=pattern permission rules")
	flags.BoolVar(&o.yes, "yes", false, "deny permission prompts in unattended runs")
	flags.DurationVar(&o.timeout, "run-timeout", 5*time.Minute, "execution deadline; 0 disables, cancellation is cooperative")
	if err := flags.Parse(args[1:]); err != nil {
		return o, err
	}
	if flags.NArg() != 1 || flags.Arg(0) == "" {
		return o, fmt.Errorf("%s", workflowUsage)
	}
	if o.timeout < 0 {
		return o, fmt.Errorf("-run-timeout must not be negative")
	}
	o.recipe = flags.Arg(0)
	return o, buildPolicy(o.allow, o.deny).Validate()
}

// executeWorkflow never constructs an LLM client, starts adapters or reads model
// preferences. Outer, nested and generated-file permissions still apply.
func executeWorkflow(ctx context.Context, o workflowOptions, out io.Writer, gate permission.Gate) error {
	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ws, err := workspace.New(o.workspace)
	if err != nil {
		return err
	}
	defer ws.Close()
	policy := buildPolicy(o.allow, o.deny)
	if err := policy.Validate(); err != nil {
		return err
	}
	authorize := func(name string, args json.RawMessage) error {
		paths, err := tools.PathResources(ws, name, args)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if err := permission.Authorize(ctx, policy, gate, name, path, args); err != nil {
				return err
			}
		}
		return nil
	}
	var values map[string]json.RawMessage
	if o.inputs != "" {
		args, _ := json.Marshal(map[string]string{"path": o.inputs})
		if err := authorize("read_file", args); err != nil {
			return err
		}
		f, err := ws.Open(o.inputs)
		if err != nil {
			return err
		}
		info, statErr := f.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
			f.Close()
			return fmt.Errorf("workflow inputs must be a regular JSON file of at most 64 KiB")
		}
		data, readErr := io.ReadAll(io.LimitReader(f, (64<<10)+1))
		closeErr := f.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		if len(data) > 64<<10 {
			return fmt.Errorf("workflow inputs exceed 64 KiB")
		}
		if err := json.Unmarshal(data, &values); err != nil {
			return fmt.Errorf("workflow inputs: %w", err)
		}
		if values == nil {
			return fmt.Errorf("workflow inputs must be a JSON object")
		}
	}
	args, err := json.Marshal(struct {
		Path   string                     `json:"recipe_path"`
		Inputs map[string]json.RawMessage `json:"inputs,omitempty"`
	}{o.recipe, values})
	if err != nil {
		return err
	}
	t := tools.NewWorkflow(ws, o.validateOnly, policy, gate)
	if o.storeDir != "" {
		t.StoreDir = o.storeDir
	}
	if err := authorize(t.Name(), args); err != nil {
		return err
	}
	result, runErr := t.Run(ctx, args)
	if result != "" {
		if _, err := fmt.Fprintln(out, result); err != nil {
			return errors.Join(runErr, err)
		}
	}
	return runErr
}
