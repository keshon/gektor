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
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/mcpmanager"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/tui"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workspace"
)

// runAgentLaunch composes workspace tools and owns their per-launch cleanup.
func runAgentLaunch(launch launchRequest, o resolvedLaunch) error {
	task := o.task
	if task == "" && o.resume == "" && o.fork == "" && !o.serve && !o.tui {
		return fmt.Errorf(`usage: gektor [flags] "task description"  (or  gektor -resume <state.json> [-answer "..."])`)
	}

	root, err := resumeWorkspace(o)
	if err != nil {
		return err
	}
	o.workspace = root
	capabilityConfig, err := loadAgentCapabilities(o)
	if err != nil {
		return err
	}
	skillRoots := append(workspace.DefaultSkillRoots(), capabilityConfig.SkillRoots...)
	ws, wsErr := workspace.NewWithSkillRoots(o.workspace, skillRoots)
	if wsErr != nil {
		return fmt.Errorf("workspace: %w", wsErr)
	}
	defer ws.Close()
	if o.workflowRecipe != "" {
		path, err := ws.Relative(o.workflowRecipe)
		if err != nil {
			return err
		}
		if path == "." {
			return fmt.Errorf("recipe path must name a file")
		}
	}
	capabilities, closeCapabilities := tools.BuildCapabilities(capabilityConfig, ws.Root())
	defer closeCapabilities()
	reserved, _, _, _ := tools.ParseMCPFlag(o.mcp)
	for _, p := range capabilityConfig.Packs {
		reserved = append(reserved, p.ID)
	}
	if o.browser != "" {
		reserved = append(reserved, "browser")
	}
	managedMCP, err := mcpmanager.New(userdirs.Sub("mcp"), ws.Root(), userdirs.Sub("credentials"), reserved)
	if err != nil {
		return err
	}
	defer managedMCP.Close()

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
	if o.outputMode == "json" && !o.serve && !o.tui {
		emitter = events.New(os.Stdout)
		emitter.Emit("run_start", map[string]any{
			"task": task, "backend": o.backendKind, "model": o.model,
			"context_limit": o.contextLimit, "workspace": o.workspace,
		})
		defer emitter.Emit("run_end", nil)
	}

	// Every run snapshots its history to disk after each step, so a crash
	// or hitting MaxSteps doesn't lose everything — see -resume. -fork
	// loads a prior transcript but writes to a fresh id (a branch).
	stateFile, forkHistory, err := agentLaunchHistory(o, note)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for _, server := range managedMCP.List() {
		if server.Enabled && server.Startup {
			if _, e := managedMCP.Connect(ctx, server.ID); e != nil {
				fmt.Fprintf(os.Stderr, "mcp %s: %v\n", server.ID, e)
			}
		}
	}
	client, err := app.NewBackend(ctx, app.BackendOptions{Kind: o.backendKind, URL: o.backendURL, Model: o.model, APIKey: o.apiKey, APIKeyEnv: o.apiKeyEnv, NoGrammar: !o.grammar, ReasoningEffort: o.reasoningEffort, ReasoningSummary: o.reasoningSummary})
	if err != nil {
		return err
	}
	if o.debug {
		f, err := os.Create("agent-debug.log")
		if err != nil {
			return fmt.Errorf("open debug log: %w", err)
		}
		defer f.Close()
		client.SetDebug(f)
		note("debug: logging raw request/response JSON to agent-debug.log")
	}

	accountLabel := client.AccountLabel
	if accountLabel != "" {
		note("ChatGPT account: %s", accountLabel)
	}

	facts, err := inspectLaunchBackend(ctx, client, o, emitter, note)
	if err != nil {
		return err
	}

	// Shared across both tool sets so a subagent's start_background and
	// the parent's check_background/stop_background see the same
	// processes — a process started by one half of the conversation
	// should be checkable/stoppable from the other.
	bgProcs := tools.NewBackgroundProcesses()
	defer bgProcs.StopAll()

	mcpTools, mcpClients := discoverMCP(ctx, o.mcp, ws.Root())
	defer func() {
		for _, c := range mcpClients {
			c.Close()
		}
	}()
	var browser *tools.Browser
	if o.browser != "" {
		if len(tools.BrowserToolNames(mcpTools)) > 0 {
			return fmt.Errorf("use either -browser or browser tools configured through -mcp, not both")
		}
		browser = &tools.Browser{Channel: o.browser, Dir: ws.Root(), Profile: o.browserProfile, ProfileExplicit: o.explicitFlags["browser-profile"], Headless: o.browserHeadless}
		defer browser.Close()
		note("browser: %s available on request through enable_browser", o.browser)
	} else if len(tools.BrowserToolNames(mcpTools)) == 0 {
		note("browser: not connected (enable with -browser chrome or -browser msedge)")
	}

	base := roles.Env{
		Client: client, RefreshModelFacts: client.RefreshModelFacts, WS: ws, Procs: bgProcs,
		ContextLimit: facts.contextLimit, Model: facts.model, Vision: facts.vision, VisionKnown: facts.visionKnown,
		MCPTools: mcpTools, Browser: browser, Capabilities: capabilities, CapabilitySources: managedMCP.Sources,
	}
	cfg, err := launchRunConfiguration(o, base, note)
	if err != nil {
		return err
	}

	cfg.StateFile = stateFile
	if o.serve {
		return serveAgentLaunch(ctx, o, cfg)
	}
	// Images resolve against the workspace (escape-checked like every
	// file tool path) into absolute paths the backend layer reads.
	images, err := agentLaunchImages(ws, o.images)
	if err != nil {
		return err
	}

	// All terminal modes share the configured environment and saved history.
	if o.tui {
		cfg.Images = images
		tuiCfg := tuiConfiguration(launch, o, cfg, forkHistory)
		tuiCfg.MCP, tuiCfg.AccountLabel = managedMCP, accountLabel
		return runAgentTUI(ctx, launch, o, tuiCfg, note)
	}

	cfg.Images, cfg.Answer = images, stdioSuspender(noteW, emitter)
	cfg.Env.Gate = permission.LogGate(o.auditPath, "cli", permissionGateContext(o.yes, cfg.Answer, ws))
	return runAgentCLI(ctx, o, cfg, forkHistory, emitter, noteW, note)
}

// resumeWorkspace resolves archive metadata before composing workspace services.
func resumeWorkspace(o resolvedLaunch) (string, error) {
	if o.resume != "" {
		snapshot, err := conversation.Load(o.resume)
		if err != nil {
			return "", fmt.Errorf("resume: %w", err)
		}
		if o.explicitFlags["workspace"] {
			snapshot.Workspace, err = filepath.Abs(o.workspace)
			if err != nil {
				return "", err
			}
		}
		root, err := snapshot.ResolveWorkspace()
		if err != nil {
			return "", fmt.Errorf("resume: %w; use -workspace <directory> -resume <state.json> to reselect", err)
		}
		return root, nil
	}
	return o.workspace, nil
}

// Persist only after workspace and backend services have been composed successfully.
func persistSwitchedWorkspace(launch launchRequest, root string) error {
	if !launch.reassignWorkspace || launch.directory == "" {
		return nil
	}
	path := filepath.Join(launch.directory, "state.json")
	snapshot, err := conversation.Load(path)
	if os.IsNotExist(err) {
		// A fresh conversation can have a reserved directory but no archive yet.
		// Its first persistence will record the newly composed workspace.
		return nil
	}
	if err != nil {
		return err
	}
	snapshot.Workspace = root
	if _, err := snapshot.ResolveWorkspace(); err != nil {
		return err
	}
	return snapshot.Save(path)
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
	extra = append(extra, parseRuleSpecs(allow, permission.Allow)...)
	extra = append(extra, parseRuleSpecs(deny, permission.Deny)...)
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

// Local servers own their actual window; configured limits are fallbacks.
func backendContext(ctx context.Context, client interface {
	MaxContextLength(context.Context) (int, error)
}, o resolvedLaunch) (int, error) {
	return app.ContextLimit(ctx, o.backendKind, o.backendURL, o.model, o.contextLimit, client)
}

func loadAgentCapabilities(o resolvedLaunch) (tools.CapabilityConfig, error) {
	if err := buildPolicy(o.allow, o.deny).Validate(); err != nil {
		return tools.CapabilityConfig{}, err
	}
	if _, _, _, err := tools.ParseMCPFlag(o.mcp); err != nil {
		return tools.CapabilityConfig{}, err
	}
	var capabilityConfig tools.CapabilityConfig
	if o.capabilities != "" {
		configPath, err := filepath.Abs(o.capabilities)
		if err != nil {
			return tools.CapabilityConfig{}, err
		}
		capabilityConfig, err = tools.LoadCapabilityConfig(configPath)
		if err != nil {
			return tools.CapabilityConfig{}, err
		}
	}
	for _, pack := range capabilityConfig.Packs {
		if pack.ID == "browser" && o.browser != "" {
			return tools.CapabilityConfig{}, fmt.Errorf("capability ID browser is reserved while -browser is enabled")
		}
	}
	return capabilityConfig, nil
}

func agentLaunchHistory(o resolvedLaunch, note func(string, ...any)) (string, []llm.Message, error) {
	var stateFile string
	var forkHistory []llm.Message
	if o.resume != "" {
		stateFile = o.resume
		if info, err := os.Stat(stateFile); err == nil && info.IsDir() {
			stateFile = filepath.Join(stateFile, "state.json")
		}
	} else if !o.serve && (!o.tui || o.task != "" || o.fork != "") {
		sum := sha1.Sum([]byte(o.task + time.Now().String()))
		taskID := hex.EncodeToString(sum[:])[:8]
		taskDir := userdirs.Sub("tasks", taskID)
		stateFile = filepath.Join(taskDir, "state.json")
		// The session title lives with the session (pi's session_info):
		// written once here, rewritten on TUI rename, derived from
		// history when absent. Display metadata, never load-bearing.
		_ = conversation.WriteSessionTitle(taskDir, conversation.TitleLine(o.task))
		if o.fork != "" {
			var err error
			forkHistory, err = conversation.ReadHistory(o.fork)
			if err != nil {
				return "", nil, fmt.Errorf("fork: %w", err)
			}
			note("forked from %s (%d messages)", o.fork, len(forkHistory))
		}

		if o.workflowRecipe != "" {
			note("workflow draft session: %s (history: %s)", taskID, stateFile)
		} else {
			note("task id: %s (resume with -resume %s)", taskID, stateFile)
		}
	}

	return stateFile, forkHistory, nil
}

func agentLaunchImages(ws *workspace.Workspace, raw string) ([]string, error) {
	var images []string
	if strings.TrimSpace(raw) != "" {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			full, err := ws.Resolve(p)
			if err != nil {
				return nil, fmt.Errorf("image: %w", err)
			}
			images = append(images, full)
		}
	}

	return images, nil
}

func runAgentTUI(ctx context.Context, launch launchRequest, o resolvedLaunch, cfg tui.Config, note func(string, ...any)) error {
	history := cfg.History
	if o.resume != "" {
		var err error
		history, err = conversation.ReadHistory(o.resume)
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
		profile = launchProfile(o, cfg.Env.ContextLimit)
		note("no -profile: using a generated one so Settings stays reachable")
	}
	if cfg.Env.ContextLimit > 0 {
		profile.Generation.ContextLimit = cfg.Env.ContextLimit
	}

	prepared, err := app.PrepareProfile(profile, filepath.Join(filepath.Dir(launch.preferencePath), "sessions"))
	if err != nil {
		return err
	}
	if err := persistSwitchedWorkspace(launch, o.workspace); err != nil {
		return fmt.Errorf("save session workspace: %w", err)
	}
	services := prepared
	app := applicationControl(launch, profile, o.profilePath, o.workspace)
	cfg.Application, cfg.PreparedProfile, cfg.History = app, &services, history
	answer, err := tui.Run(ctx, cfg)
	if err != nil {
		return fmt.Errorf("agent failed: %w", err)
	}
	if strings.TrimSpace(answer) != "" {
		fmt.Println("\n=== result ===")
		fmt.Println(answer)
	}
	return nil
}

func reportAgentRun(r agent.RunReport, stateFile string, o resolvedLaunch, note func(string, ...any)) {
	if r.Summary != "" {
		note("%s", r.Summary)
	}
	note("outcome: %s; changed files: %s", r.Outcome, strings.Join(r.MutatedPaths, ", "))
	if r.Verification != "" {
		note("checks: %s", r.Verification)
	} else {
		note("checks: no current command verdict")
	}
	for _, warning := range r.Warnings {
		note("%s", warning)
	}
	if o.workflowRecipe != "" {
		note("draft history: %s; workflow draft sessions do not support resume", stateFile)
	} else {
		note("resume: gektor -resume %s", stateFile)
	}
}
