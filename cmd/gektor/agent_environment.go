package main

import (
	"context"
	"strings"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/tui"
)

type launchModelFacts struct {
	contextLimit        int
	model               string
	vision, visionKnown bool
}

func inspectLaunchBackend(ctx context.Context, client *app.Backend, o resolvedLaunch, emitter *events.Emitter, note func(string, ...any)) (launchModelFacts, error) {
	contextLimit, err := backendContext(ctx, client, o)
	if err != nil {
		return launchModelFacts{}, err
	}
	note("context limit: %d tokens", contextLimit)
	emitter.Emit("context", map[string]any{"context_limit": contextLimit})
	displayModel := o.model
	if name, err := client.ModelName(ctx); err == nil && strings.TrimSpace(name) != "" {
		displayModel = name
	}
	// Capability, not a flag: a text-only backend must not be offered a
	// read_file that would attach pictures and fail the turn at the wire
	// encoder. An unknown answer is treated as "no".
	vision, err := client.SupportsVision(ctx)
	visionKnown := err == nil
	if err != nil {
		vision = false
	}
	if vision {
		note("vision: image files can be read by the agent")
	}

	return launchModelFacts{contextLimit, displayModel, vision, visionKnown}, nil
}

// launchRunConfiguration prepares settings before choosing a frontend.
func launchRunConfiguration(o resolvedLaunch, base roles.Env, note func(string, ...any)) (harness.Config, error) {
	identity, warnings, err := app.CompileIdentity(app.IdentityOptions{Preset: selectedProfilePreset(o.selectedProfile), Name: selectedProfileName(o.selectedProfile), Persona: selectedProfilePersona(o.selectedProfile), User: o.userName, System: o.systemPrompt, Post: o.postHistory, MaxTokens: o.maxTokens, ContextLimit: base.ContextLimit, Temperature: o.temperature})
	if err != nil {
		return harness.Config{}, err
	}
	for _, warning := range warnings {
		note("Identity warning: " + warning)
	}
	env := base
	env.Identity, env.IdentityName, env.UserName = app.IdentityPrompt(identity), identity.Name, o.userName
	env.MaxTokens, env.Temperature = o.maxTokens, o.temperature
	env.MaxSteps, env.DelegateSteps, env.DelegateMaxSteps = o.maxSteps, o.delegateSteps, o.delegateMaxSteps
	env.RunTimeout, env.Policy = o.runTimeout, buildPolicy(o.allow, o.deny)
	env.BackendKind = o.backendKind
	env.Stream = o.stream || o.tui
	env.Rules = checks.Secrets()
	return harness.Config{Env: env, Task: o.task, Plan: o.plan, WorkflowRecipe: o.workflowRecipe, Verify: verificationCommand(o.verifyCmd, o.workspace), ResumeAnswer: o.answer}, nil
}

func tuiConfiguration(launch launchRequest, o resolvedLaunch, cfg harness.Config, history []llm.Message) tui.Config {
	return tui.Config{
		StartScreen: o.tuiScreen, SeedHistory: launch.history,
		InitialDraft: launch.draft, InitialImages: launch.images, Drafts: launch.drafts, SessionDirectory: launch.directory,
		BackendURL: o.backendURL, Plan: cfg.Plan, Fork: o.fork != "",
		History: history, ResumeAnswer: cfg.ResumeAnswer, AuditPath: o.auditPath,
		Env: cfg.Env, Task: cfg.Task, StateFile: cfg.StateFile, Verify: cfg.Verify, Images: cfg.Images,
	}
}
