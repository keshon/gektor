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

	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/api"
	"github.com/keshon/tars/internal/app"
	"github.com/keshon/tars/internal/audit"
	"github.com/keshon/tars/internal/events"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/mission"
	"github.com/keshon/tars/internal/permission"
	"github.com/keshon/tars/internal/prompts"
	"github.com/keshon/tars/internal/roles"
	"github.com/keshon/tars/internal/session"
	"github.com/keshon/tars/internal/snapshot"
	"github.com/keshon/tars/internal/tools"
	"github.com/keshon/tars/internal/tui"
	"github.com/keshon/tars/internal/workspace"
)

// runAgentLaunch composes workspace tools and owns their per-launch cleanup.
func runAgentLaunch(launch launchRequest, o resolvedLaunch) error {
	task := o.task
	if task == "" && o.resume == "" && o.fork == "" && !o.serve && !o.tui && !o.revert && !o.revertPreview {
		return fmt.Errorf(`usage: agent [flags] "task description"  (or  agent -resume <state.json> [-answer "..."])`)
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

	// The event emitter must exist before either mode branches: mission
	// output below needs it, and run_end must fire for both modes.
	var emitter *events.Emitter
	if o.outputMode == "json" {
		emitter = events.New(os.Stdout)
		emitter.Emit("run_start", map[string]any{
			"task": task, "backend": o.backendKind, "model": o.model,
			"context_limit": o.contextLimit, "workspace": o.workspace,
		})
		defer emitter.Emit("run_end", nil)
	}

	// One suspender for every gate in the run: ask_user questions,
	// permission prompts, and the mission plan approval below all ask
	// through it. A single shared stdin reader matters — three buffered
	// readers on one stdin would eat each other's input.
	suspend := stdioSuspender(noteW, emitter)

	if !o.missionMode && !o.direct && task != "" && mission.SuggestMission(task) {
		note("task spans multiple files; use -mission for a reviewed execution plan")
	}

	// A -resume target whose directory holds mission.json is a mission
	// resume, whatever the path points at (the dir itself, mission.json,
	// or a worker state file) — direct-mode resume stays the fallback.
	missionDir := ""
	if o.resume != "" {
		cand := o.resume
		if info, err := os.Stat(cand); err != nil || !info.IsDir() {
			cand = filepath.Dir(cand)
		}
		if mission.Exists(cand) {
			missionDir = cand
		}
	}

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
	} else if !o.revert && !o.revertPreview && !o.serve && (!o.tui || task != "" || o.fork != "") {
		sum := sha1.Sum([]byte(task + time.Now().String()))
		taskID := hex.EncodeToString(sum[:])[:8]
		taskDir := filepath.Join(ws.Root(), workspace.TaskDir(taskID))
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
		if o.missionMode {
			missionDir = taskDir
			note("mission id: %s (resume with -resume %s)", taskID, taskDir)
		} else {
			note("task id: %s (resume with -resume %s)", taskID, stateFile)
		}
	}

	if o.revert || o.revertPreview {
		actions, err := snapshot.Preview(ws.Root(), o.revertFrom)
		if err != nil {
			return fmt.Errorf("checkpoint: %w", err)
		}
		for _, action := range actions {
			fmt.Fprintln(noteW, action)
		}
		if o.revertPreview {
			return nil
		}
		if o.revertFrom != "" {
			err = snapshot.Restore(ws.Root(), o.revertFrom)
		} else {
			err = snapshot.Revert(ws.Root())
		}
		if err != nil {
			return fmt.Errorf("revert: %w", err)
		}
		note("workspace restored to the pre-run checkpoint")
		return nil
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

	var contextLimit int
	if o.backendKind == "openai" {
		// Hosted gateways expose no probe endpoint, so the window comes
		// from the flag or the per-model estimate. A local server answering
		// here still means the kind is wrong, so keep that tripwire.
		if o.contextLimit > 0 {
			contextLimit = o.contextLimit
		} else {
			contextLimit = llm.OpenAIContextLimit(o.model)
		}
		note("context window: %d tokens (estimated for %s; override with -context-limit)",
			contextLimit, o.model)
		if actual := llm.DetectKind(ctx, o.backendURL); actual != "" && actual != o.backendKind {
			return fmt.Errorf("-backend-kind is %q but %s is answering at %s.\n"+
				"Re-run with -backend-kind %s. Continuing would send %s's grammar and\n"+
				"sampler fields to a server that ignores both, and the run would look fine.",
				o.backendKind, actual, o.backendURL, actual, o.backendKind)
		}
	} else {
		var err error
		contextLimit, err = client.MaxContextLength(ctx)
		if err != nil {
			// The probe failing is evidence, not noise: each dialect asks a
			// different endpoint, so if the OTHER one answers, -backend-kind
			// is wrong. Continuing is worse than stopping, because the
			// dialects disagree about the grammar field, the sampler field
			// names and the structured-output mechanism: the run completes
			// having measured nothing. Nine mission runs did exactly that
			// before this check existed.
			if actual := llm.DetectKind(ctx, o.backendURL); actual != "" && actual != o.backendKind {
				return fmt.Errorf("-backend-kind is %q but %s is answering at %s.\n"+
					"Re-run with -backend-kind %s. Continuing would send %s's grammar and\n"+
					"sampler fields to a server that ignores both, and the run would look fine.",
					o.backendKind, actual, o.backendURL, actual, o.backendKind)
			}
			// Otherwise the backend is simply not reporting: keep going
			// without budget tracking rather than failing the whole run.
			contextLimit = 0
		} else {
			note("context window: %d tokens", contextLimit)
		}
	}
	emitter.Emit("context", map[string]any{"context_limit": contextLimit})

	// Reuses mission.RunShellCommand for the same OS-aware shell choice as
	// tools.RunShell and mission shell checks — one exec shape everywhere.
	var verify func(ctx context.Context) (string, bool)
	if o.verifyCmd != "" {
		verify = func(ctx context.Context) (string, bool) {
			out, err := mission.RunShellCommand(ctx, o.verifyCmd, ws.Root())
			status := "PASSED"
			if err != nil {
				status = "FAILED"
			}
			return fmt.Sprintf("%s\ncommand: %s\n%s", status, o.verifyCmd, out), true
		}
	}

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
			"[resume: agent -resume %s -answer \"your answer\"]\n"+
			"\n[agent asks] %s", stateFile, stateFile, question)
	})

	// Git snapshot before any work, so the run is reviewable/revertible.
	// Skipped in serve mode: each served run snapshots into its own
	// task dir instead, and this stateFile belongs to no run.
	if !o.serve && stateFile != "" {
		if snap := snapshot.Track(ws.Root(), filepath.Join(filepath.Dir(stateFile), "snapshots")); snap.Path != "" {
			note("snapshot: %s", snap.Path)
		} else {
			note("pre-run checkpoint unavailable; this run cannot be undone with -revert")
		}
	}

	if o.serve {
		// stdout is the protocol in serve mode regardless of -mode:
		// human chatter always goes to stderr.
		serveMain(ctx, serveDeps{
			client:       client,
			ws:           ws,
			procs:        bgProcs,
			maxTokens:    o.maxTokens,
			contextLimit: contextLimit,
			logMax:       o.logMax,
			verifyCmd:    o.verifyCmd,
			autoApprove:  o.yes,
			autoDeny:     o.yes,
			auditPath:    o.auditPath,
			allow:        o.allow,
			deny:         o.deny,
			backendKind:  o.backendKind,
			stream:       o.stream,
			thinkBudget:  o.thinkBudget,
			skipVerify:   o.noVerify,
			mcpTools:     mcpTools,
		}, os.Stdin, os.Stdout, os.Stderr)
		return ctx.Err()
	}

	thinkingClient := client
	// Identity and response style come from the profile; the agent's own
	// Plan/Act instructions are added on top and do not compete with it.
	identity, warnings, err := app.Compile(app.IdentityOptions{Preset: selectedProfilePreset(o.selectedProfile), Name: selectedProfileName(o.selectedProfile), Persona: selectedProfilePersona(o.selectedProfile), User: o.userName, System: o.systemPrompt, Post: o.postHistory, MaxTokens: o.maxTokens, ContextLimit: contextLimit, Temperature: o.temperature})
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		note("Identity warning: " + warning)
	}
	env := roles.Env{
		Identity:        app.IdentityPrompt(identity),
		IdentityName:    identity.Name,
		UserName:        o.userName,
		Client:          thinkingClient,
		WS:              ws,
		Procs:           bgProcs,
		MaxTokens:       o.maxTokens,
		Temperature:     o.temperature,
		ContextLimit:    contextLimit,
		MaxSteps:        o.maxSteps,
		Policy:          buildPolicy(o.allow, o.deny),
		GateContext:     audit.ContextHook(o.auditPath, "cli", permissionGateContext(o.yes, suspend, ws)),
		BackendKind:     o.backendKind,
		Model:           o.model,
		Stream:          streamEnabled(o.stream, o.outputMode, o.tui),
		MCPTools:        mcpTools,
		ReasoningBudget: o.thinkBudget,
		// Direct runs fund acknowledged todo work with extra steps;
		// -serve leaves this off until its budget story is decided.
		TodoFunding: true,
		SkipVerify:  o.noVerify,
		OnDelta:     func(chunk string) { fmt.Print(chunk) },
		OnStep:      stepPrinter(o.outputMode, emitter, o.logMax),
		OnResult: func(result agent.ToolResult) {
			if o.outputMode == "json" {
				api.EmitToolResult(emitter, result.CallID, result.Output, result.Failed)
			}
		},
		OnUsage:   usagePrinter(o.outputMode, emitter),
		OnFinding: findingPrinter(o.outputMode),
		OnNudge:   nudgePrinter(o.outputMode),
	}
	if missionDir != "" {
		params := missionParams{
			env:          env,
			client:       client,
			ws:           ws,
			procs:        bgProcs,
			dir:          missionDir,
			task:         task,
			resuming:     o.resume != "",
			contextLimit: contextLimit,
			maxTokens:    o.maxTokens,
			logMax:       o.logMax,
			verifyCmd:    o.verifyCmd,
			autoApprove:  o.yes,
			noteW:        noteW,
			emitter:      emitter,
			suspend:      suspend,
		}
		if o.tui {
			_, err := tui.Run(ctx, tui.Config{BackendURL: o.backendURL, Env: env, Task: task, StateFile: filepath.Join(missionDir, "mission.json"), AuditPath: o.auditPath, Mission: func(runCtx context.Context, runEnv roles.Env, answer agent.Suspender, emitter *events.Emitter) error {
				p := params
				p.env = runEnv
				p.suspend = answer
				p.emitter = emitter
				p.noteW = io.Discard
				return runMission(runCtx, p)
			}})
			if err != nil {
				return fmt.Errorf("mission: %w", err)
			}
		} else if err := runMission(ctx, params); err != nil {
			return fmt.Errorf("mission: %w", err)
		}
		return nil
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

		prepared, err := app.ConfigureAgent(profile, filepath.Join(filepath.Dir(launch.preferencePath), "sessions"))
		if err != nil {
			return err
		}
		prepared.Client = thinkingClient
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
		a = roles.Planner(env, "plan", stateFile)
	} else {
		a = roles.Interactive(env, "", stateFile, askFn, verify)
	}

	defer func() {
		r := a.Report()
		api.EmitOutcome(emitter, r, stateFile)
		note("outcome: %s; changed files: %s", r.Outcome, strings.Join(r.MutatedPaths, ", "))
		if r.Verification != "" {
			note("checks: %s", r.Verification)
		} else {
			note("checks: no current command verdict")
		}
		for _, warning := range r.Warnings {
			note("%s", warning)
		}
		note("resume: agent -resume %s", stateFile)
	}()

	var result string
	if o.resume != "" {
		history, err := session.ReadHistory(o.resume)
		if err != nil {
			return fmt.Errorf("resume: %w", err)
		}
		note("resuming from %s (%d messages)", o.resume, len(history))

		if callID, question, paused := agent.PausedOnQuestion(history); paused {
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
		result, err = a.Resume(ctx, forkHistory, prompts.Resume)
		if err != nil {
			return fmt.Errorf("agent failed: %w", err)
		}
	} else {
		var err error
		result, err = a.Run(ctx, task, images...)
		if err != nil {
			return fmt.Errorf("agent failed: %w", err)
		}
	}
	// Printed whole. -log-max caps each *step* line so a run stays
	// readable while it scrolls past; the final answer is the thing the
	// run was for, and middle-truncating it to 300 characters cut the
	// deliverable out of its own report. Mission mode already printed its
	// report in full, so this also makes the two modes agree.
	note("\n=== result ===")
	fmt.Fprintln(noteW, result)
	return nil
}

type missionParams struct {
	env          roles.Env
	client       llm.Client
	ws           *workspace.Workspace
	procs        *tools.BackgroundProcesses
	dir          string
	task         string
	resuming     bool
	contextLimit int
	maxTokens    int
	logMax       int
	verifyCmd    string
	autoApprove  bool
	// noteW carries human-readable output (stderr in -mode json, so
	// stdout stays pure JSONL). emitter is nil unless -mode json.
	noteW   io.Writer
	emitter *events.Emitter
	// suspend answers the plan approval gate. Same shared suspender as
	// the direct-mode gates: one stdin reader per run.
	suspend agent.Suspender
}

// runMission is the -mission entry point: harness-owned plan → execute →
// verify instead of one long reactive conversation. The interaction
// point with the human is the plan approval gate; workers themselves
// never ask questions.
func runMission(ctx context.Context, p missionParams) error {
	var m *mission.Mission
	if p.resuming {
		var err error
		m, err = mission.Load(p.dir)
		if err != nil {
			return fmt.Errorf("resume mission: %w", err)
		}
		progress := ""
		if len(m.Subtasks) > 0 {
			progress = fmt.Sprintf(", subtask %d/%d", m.Cursor+1, len(m.Subtasks))
		}
		fmt.Fprintf(p.noteW, "resuming mission %s (phase: %s%s)\n", m.ID, m.Phase, progress)
	} else {
		m = &mission.Mission{ID: filepath.Base(p.dir), Task: p.task, Phase: mission.PhaseExplore}
		if err := m.Save(p.dir); err != nil {
			return fmt.Errorf("create mission: %w", err)
		}
	}

	// The approval gate is the cheapest, strongest defense against a
	// weak model's garbage plans: a human reads it before anything runs.
	var approve func(string) (bool, string)
	if !p.autoApprove {
		approve = func(rendered string) (bool, string) {
			rep, err := p.suspend(ctx, agent.SuspendRequest{
				Kind:   agent.SuspendPlan,
				Prompt: "=== proposed plan ===\n" + rendered + "\n\napprove? [y]es / [n]o / or type a revision note",
			})
			if err != nil {
				return false, ""
			}
			line := strings.TrimSpace(rep.Answer)
			switch strings.ToLower(line) {
			case "y", "yes":
				return true, ""
			case "n", "no":
				return false, ""
			default:
				return false, line
			}
		}
	}

	runner := &mission.Runner{
		Env:          p.env,
		Client:       p.client,
		WS:           p.ws,
		Dir:          p.dir,
		Procs:        p.procs,
		ContextLimit: p.contextLimit,
		MaxTokens:    p.maxTokens,
		ApprovePlan:  approve,
		VerifyCmd:    p.verifyCmd,
		OnStep: func(subID string, step int, msg llm.Message) {
			api.EmitStep(p.emitter, subID, step, msg, false, 0)
			if msg.Content != "" {
				fmt.Fprintf(p.noteW, "[%s step %d] %s\n", subID, step, agent.TruncateMiddle(msg.Content, p.logMax))
			}
			for _, tc := range msg.ToolCalls {
				fmt.Fprintf(p.noteW, "[%s step %d] -> %s(%s)\n", subID, step, tc.Name,
					agent.TruncateMiddle(string(tc.Arguments), p.logMax))
			}
		},
		OnEvent: func(format string, args ...any) {
			if p.emitter != nil {
				p.emitter.Emit("mission", map[string]any{"text": fmt.Sprintf(format, args...)})
			}
			fmt.Fprintf(p.noteW, "[mission] "+format+"\n", args...)
		},
		OnResult: func(result agent.ToolResult) {
			api.EmitToolResult(p.emitter, result.CallID, result.Output, result.Failed)
		},
		OnUsage: func(step int, usage llm.Usage) {
			api.EmitUsage(p.emitter, step, usage)
		},
	}

	report, err := runner.Run(ctx, m)
	if p.emitter != nil {
		p.emitter.Emit("result", map[string]any{"report": events.Message(report)})
	}
	fmt.Fprintln(p.noteW, "\n=== mission report ===")
	fmt.Fprintln(p.noteW, report)
	return err
}

// buildPolicy combines built-in defaults with -allow/-deny overrides.
// Extra rules always win by append order.
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
