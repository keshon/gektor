package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/audit"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/rules"
	"github.com/keshon/gektor/internal/run"
	"github.com/keshon/gektor/internal/session"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/tui"
	"github.com/keshon/gektor/internal/workspace"
)

// runAgentLaunch composes workspace tools and owns their per-launch cleanup.
func runAgentLaunch(launch launchRequest, o resolvedLaunch) error {
	task := o.task
	if task == "" && o.resume == "" && o.fork == "" && !o.serve && !o.tui {
		return fmt.Errorf(`usage: gektor [flags] "task description"  (or  gektor -resume <state.json> [-answer "..."])`)
	}

	ws, wsErr := workspace.New(o.workspace)
	if wsErr != nil {
		return fmt.Errorf("workspace: %w", wsErr)
	}

	// noteW is where human-readable chatter goes. In -mode json it is
	// stderr, so stdout carries nothing but JSONL and a caller can pipe
	// it straight into a parser; in print mode it is stdout as before.
	// -serve always uses stderr: stdout is the protocol.
	noteW := io.Writer(os.Stdout)
	if o.outputMode == "json" || o.serve {
		noteW = os.Stderr
	}
	note := func(format string, args ...any) {
		fmt.Fprintf(noteW, format+"\n", args...)
	}

	var emitter *events.Emitter
	if o.outputMode == "json" {
		emitter = events.New(os.Stdout)
		emitter.Emit("run_start", map[string]any{
			"task": task, "backend": o.backendKind, "model": o.model,
			"context_limit": o.contextLimit, "workspace": o.workspace,
		})
		defer emitter.Emit("run_end", nil)
	}

	suspend := stdioSuspender(noteW, emitter)

	// Every run snapshots its history to disk after each step, so a crash
	// or hitting MaxSteps doesn't lose everything — see -resume. -fork
	// loads a prior transcript but writes to a fresh id (a branch).
	var stateFile string
	var forkHistory []llm.Message
	if o.resume != "" {
		stateFile = o.resume
		if info, err := os.Stat(stateFile); err == nil && info.IsDir() {
			stateFile = filepath.Join(stateFile, "state.json")
		}
	} else if !o.serve && (!o.tui || task != "" || o.fork != "") {
		sum := sha1.Sum([]byte(task + time.Now().String()))
		taskID := hex.EncodeToString(sum[:])[:8]
		taskDir := gektor.Sub("tasks", taskID)
		stateFile = filepath.Join(taskDir, "state.json")
		// The session title lives with the session (pi's session_info):
		// written once here, rewritten on TUI rename, derived from
		// history when absent. Display metadata, never load-bearing.
		_ = workspace.WriteSessionTitle(taskDir, workspace.TitleLine(task))
		if o.fork != "" {
			var err error
			forkHistory, err = session.ReadHistory(o.fork)
			if err != nil {
				return fmt.Errorf("fork: %w", err)
			}
			note("forked from %s (%d messages)", o.fork, len(forkHistory))
		}

		note("task id: %s (resume with -resume %s)", taskID, stateFile)
	}

	client, err := llm.ClientFor(o.backendKind, o.backendURL, o.model)
	if err != nil {
		return fmt.Errorf("%w", err)
	}
	app.ResolveAPIKey(o.apiKey, o.apiKeyEnv, client)
	if hdrs := app.ExtraHeadersFromEnv(); len(hdrs) > 0 {
		if client.ExtraHeaders == nil {
			client.ExtraHeaders = hdrs
		} else {
			for k, v := range hdrs {
				client.ExtraHeaders[k] = v
			}
		}
	}
	client.NoGrammar = !o.grammar
	if o.debug {
		f, err := os.Create("agent-debug.log")
		if err != nil {
			return fmt.Errorf("open debug log: %w", err)
		}
		defer f.Close()
		client.Debug = f
		note("debug: logging raw request/response JSON to agent-debug.log")
	}

	// Ask the backend for its real context window instead of guessing.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	contextLimit, err := backendContext(ctx, client, o)
	if err != nil {
		return err
	}
	note("context limit: %d tokens", contextLimit)
	emitter.Emit("context", map[string]any{"context_limit": contextLimit})
	displayModel := o.model
	if name, err := client.ModelName(ctx); err == nil && strings.TrimSpace(name) != "" {
		displayModel = name
	}

	verify := verificationCommand(o.verifyCmd, ws.Root())

	// Shared across both tool sets so a subagent's start_background and
	// the parent's check_background/stop_background see the same
	// processes — a process started by one half of the conversation
	// should be checkable/stoppable from the other.
	bgProcs := tools.NewBackgroundProcesses()
	defer bgProcs.StopAll()

	// Ctrl+C previously killed the agent and left whatever it had started
	// running: a dev server, a watcher, a test process. The eval harness
	// deferred StopAll and the binary people actually run did not, which
	// is also why the process-tree kill exists on Windows and was never
	// reached from here.
	//

	mcpTools, mcpClients := discoverMCP(ctx, o.mcp)
	defer func() {
		for _, c := range mcpClients {
			c.Close()
		}
	}()

	// ask_user blocks on stdin inside the tool. State is snapshotted by
	// the agent loop after the assistant message (with the unanswered tool
	// call) and before tool execution — so Ctrl+C while waiting leaves a
	// resumable state.json. Use -resume with -answer to continue.
	askFn := agent.NewAsker(0).Ask(func(_ context.Context, req agent.SuspendRequest) (agent.SuspendReply, error) {
		return suspend(ctx, req)
	}, func(question string) string {
		return fmt.Sprintf("\n[paused — state saved at %s]\n"+
			"[resume: gektor -resume %s -answer \"your answer\"]\n"+
			"\n[agent asks] %s", stateFile, stateFile, question)
	})

	if o.serve {
		// stdout is the protocol in serve mode regardless of -mode:
		// human chatter always goes to stderr.
		serveMain(ctx, serveDeps{
			client:       client,
			ws:           ws,
			procs:        bgProcs,
			maxTokens:    o.maxTokens,
			maxSteps:     o.maxSteps,
			contextLimit: contextLimit,
			logMax:       o.logMax,
			verifyCmd:    o.verifyCmd,

			autoDeny:    o.yes,
			auditPath:   o.auditPath,
			allow:       o.allow,
			deny:        o.deny,
			backendKind: o.backendKind,
			stream:      o.stream,
			mcpTools:    mcpTools,
		}, os.Stdin, os.Stdout, os.Stderr)
		return ctx.Err()
	}

	// Identity and response style come from the profile; the agent's own
	// Plan/Act instructions are added on top and do not compete with it.
	identity, warnings, err := app.Compile(app.IdentityOptions{Preset: selectedProfilePreset(o.selectedProfile), Name: selectedProfileName(o.selectedProfile), Persona: selectedProfilePersona(o.selectedProfile), User: o.userName, System: o.systemPrompt, Post: o.postHistory, MaxTokens: o.maxTokens, ContextLimit: contextLimit, Temperature: o.temperature})
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		note("Identity warning: " + warning)
	}
	// What GEKTOR checks written files for. Secrets apply to every text
	// file whatever the language; gofmt is one rule among them and lives
	// outside internal/checks so that package stays language-neutral.
	rules := append(checks.Secrets(), rules.All()...)
	env := roles.Env{
		Identity:     app.IdentityPrompt(identity),
		IdentityName: identity.Name,
		UserName:     o.userName,
		Client:       client,
		WS:           ws,
		Procs:        bgProcs,
		MaxTokens:    o.maxTokens,
		Temperature:  o.temperature,
		ContextLimit: contextLimit,
		MaxSteps:     o.maxSteps,
		Policy:       buildPolicy(o.allow, o.deny),
		GateContext:  audit.ContextHook(o.auditPath, "cli", permissionGateContext(o.yes, suspend, ws)),
		BackendKind:  o.backendKind,
		Model:        displayModel,
		Stream:       streamEnabled(o.stream, o.outputMode, o.tui),
		MCPTools:     mcpTools,
		// Direct runs fund acknowledged todo work with extra steps;
		// -serve leaves this off until its budget story is decided.
		OnDelta: func(chunk string) { fmt.Print(chunk) },
		OnStep:  stepPrinter(o.outputMode, emitter, o.logMax),
		OnToolOutput: func(callID, text string) {
			if o.outputMode == "json" {
				events.EmitToolOutput(emitter, callID, text)
			} else {
				fmt.Print(text)
			}
		},
		OnResult: func(result agent.ToolResult) {
			if o.outputMode == "json" {
				events.EmitToolResult(emitter, result.CallID, result.Output, result.Failed)
			}
		},
		OnUsage: usagePrinter(o.outputMode, emitter),
		Rules:   rules,
		Checks:  checks.NewReporter(rules, findingSink(emitter, o.outputMode)),
		OnNudge: nudgePrinter(o.outputMode),
	}

	if o.plan {
		emitter.Emit("plan_mode", map[string]any{"read_only": true})
	}
	if o.stream && o.backendKind == "kobold" {
		note("note: -stream is unsupported on koboldcpp, using unary requests")
	}

	// Images resolve against the workspace (escape-checked like every
	// file tool path) into absolute paths the backend layer reads.
	var images []string
	if strings.TrimSpace(o.images) != "" {
		for _, p := range strings.Split(o.images, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			full, err := ws.Resolve(p)
			if err != nil {
				return fmt.Errorf("image: %w", err)
			}
			images = append(images, full)
		}
	}

	// All terminal modes share the configured environment and saved history.
	if o.tui {
		history := forkHistory
		if o.resume != "" {
			var err error
			history, err = session.ReadHistory(o.resume)
			if err != nil {
				return fmt.Errorf("resume: %w", err)
			}
		}

		// A launch without -profile still needs a services handle and an
		// Application: Settings reads both, and with neither every row
		// that needs them is disabled and the menus cannot be entered
		// at all. Build the profile this launch would have loaded.
		profile := o.selectedProfile
		if profile.Version == 0 {
			profile = launchProfile(o, contextLimit)
			note("no -profile: using a generated one so Settings stays reachable")
		}
		if contextLimit > 0 {
			profile.Generation.ContextLimit = contextLimit
		}

		prepared, err := app.ConfigureAgent(profile, filepath.Join(filepath.Dir(launch.preferencePath), "sessions"))
		if err != nil {
			return err
		}
		prepared.Client = client
		services := prepared
		app := applicationControl(launch, profile, o.profilePath, o.workspace)
		answer, err := tui.Run(ctx, tui.Config{Application: app, Services: &services, SeedHistory: launch.history, InitialDraft: launch.draft, SessionDirectory: launch.directory,
			BackendURL:   o.backendURL,
			Plan:         o.plan,
			History:      history,
			ResumeAnswer: o.answer,
			AuditPath:    o.auditPath,
			Env:          env,
			Task:         task,
			StateFile:    stateFile,
			Verify:       verify,
			Images:       images,
		})
		if err != nil {
			return fmt.Errorf("agent failed: %w", err)
		}
		if strings.TrimSpace(answer) != "" {
			fmt.Println("\n=== result ===")
			fmt.Println(answer)
		}
		return nil
	}

	var a *agent.Agent
	if o.plan {
		a = roles.Planner(env, "plan", stateFile, askFn)
	} else {
		a = roles.Interactive(env, "", stateFile, askFn, verify)
	}

	defer func() {
		r := a.Report()
		// Deep pass over everything the run changed, catching writes no
		// tool reported: shell redirections, MCP, background jobs.
		env.Sweep(r.MutatedPaths)
		run.EmitOutcome(emitter, r, stateFile)
		note("outcome: %s; changed files: %s", r.Outcome, strings.Join(r.MutatedPaths, ", "))
		if r.Verification != "" {
			note("checks: %s", r.Verification)
		} else {
			note("checks: no current command verdict")
		}
		for _, warning := range r.Warnings {
			note("%s", warning)
		}
		note("resume: gektor -resume %s", stateFile)
	}()

	var result string
	if o.resume != "" {
		history, err := session.ReadHistory(o.resume)
		if err != nil {
			return fmt.Errorf("resume: %w", err)
		}
		note("resuming from %s (%d messages)", o.resume, len(history))

		// The history is off disk and no agent exists yet, so the question is
		// recognised by its argument shape rather than by a tool we could ask.
		if callID, question, paused := agent.PausedOnQuestion(nil, history); paused {
			ans := o.answer
			if ans == "" {
				var err error
				ans, err = askFn(question)
				if err != nil {
					return fmt.Errorf("reading answer: %w", err)
				}
			}
			result, err = a.ResumeWithAnswer(ctx, history, callID, ans)
		} else {
			result, err = a.Resume(ctx, history, prompts.Resume)
		}
		if err != nil {
			return fmt.Errorf("agent failed: %w", err)
		}
	} else if len(forkHistory) > 0 {
		var err error
		note := strings.TrimSpace(task)
		if note == "" {
			note = prompts.Resume
		}
		result, err = a.Resume(ctx, forkHistory, note, images...)
		if err != nil {
			return fmt.Errorf("agent failed: %w", err)
		}
	} else {
		var err error
		result, err = a.Run(ctx, task, images...)
		if err != nil {
			// A run that hit MaxSteps with work still open gets one more
			// budget if the operator says yes. This used to exist only in
			// the API session, so the primary CLI silently never offered it.
			if ans, cerr, ok := agent.BudgetContinue(ctx, a, err, stateFile, suspend); ok {
				result, err = ans, cerr
			}
		}
		if err != nil {
			return fmt.Errorf("agent failed: %w", err)
		}
	}
	note("\n=== result ===")
	fmt.Fprintln(noteW, result)
	return nil
}

func verificationCommand(command, directory string) func(context.Context) (string, error) {
	if command == "" {
		return nil
	}
	return func(ctx context.Context) (string, error) {
		out, err := tools.RunCommand(ctx, command, directory, 120*time.Second)
		status := "PASSED"
		if err != nil {
			status = "FAILED"
		}
		return fmt.Sprintf("%s\ncommand: %s\n%s", status, command, out), err
	}
}

func buildPolicy(allow, deny string) permission.Policy {
	p := permission.Default()
	var extra []permission.Rule
	for _, spec := range parseRuleSpecs(allow, permission.Allow) {
		extra = append(extra, spec)
	}
	for _, spec := range parseRuleSpecs(deny, permission.Deny) {
		extra = append(extra, spec)
	}
	return p.WithExtra(extra)
}

func parseRuleSpecs(spec string, eff permission.Effect) []permission.Rule {
	var out []permission.Rule
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tool, pattern := "*", part
		if i := strings.Index(part, "="); i >= 0 {
			tool = strings.TrimSpace(part[:i])
			pattern = strings.TrimSpace(part[i+1:])
			if tool == "" {
				tool = "*"
			}
			if pattern == "" {
				pattern = "*"
			}
		}
		out = append(out, permission.Rule{Tool: tool, Pattern: pattern, Effect: eff})
	}
	return out
}

// streamEnabled decides whether model responses stream token by
// token: explicit -stream, or the fullscreen TUI (which renders live
// text and degrades to spinner+final where the backend refuses —
// koboldcpp has no Stream method, so agent.chat falls back unary).
// Never in -mode json (chunks would corrupt the JSONL stream).
func streamEnabled(streamFlag bool, modeFlag string, tuiFlag bool) bool {
	return (streamFlag || tuiFlag) && modeFlag != "json"
}

// Local servers own their actual window; configured limits are fallbacks.
func backendContext(ctx context.Context, client *llm.Server, o resolvedLaunch) (int, error) {
	var detected int
	var probeErr error
	if o.backendKind != "openai" {
		detected, probeErr = client.MaxContextLength(ctx)
	}
	if probeErr != nil || o.backendKind == "openai" {
		if actual := llm.DetectKind(ctx, o.backendURL); actual != "" && actual != o.backendKind {
			return 0, fmt.Errorf("-backend-kind is %q but %s is answering at %s; re-run with -backend-kind %s", o.backendKind, actual, o.backendURL, actual)
		}
	}
	if probeErr == nil && detected > 0 {
		return detected, nil
	}
	if o.contextLimit > 0 {
		return o.contextLimit, nil
	}
	if o.backendKind == "openai" {
		return llm.OpenAIContextLimit(o.model), nil
	}
	if probeErr != nil {
		return 0, nil
	}
	return detected, nil
}
