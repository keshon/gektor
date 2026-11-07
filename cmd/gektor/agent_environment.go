package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/checks"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/rules"
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

func serveAgentLaunch(ctx context.Context, o resolvedLaunch, base roles.Env) {
	serveMain(ctx, serveDeps{
		client:            base.Client,
		refreshModelFacts: base.RefreshModelFacts,
		ws:                base.WS,
		procs:             base.Procs,
		maxTokens:         o.maxTokens,
		maxSteps:          o.maxSteps,
		runTimeout:        o.runTimeout,
		contextLimit:      base.ContextLimit,
		vision:            base.Vision,
		logMax:            o.logMax,
		verifyCmd:         o.verifyCmd,

		autoDeny:          o.yes,
		auditPath:         o.auditPath,
		allow:             o.allow,
		deny:              o.deny,
		backendKind:       o.backendKind,
		stream:            o.stream,
		mcpTools:          base.MCPTools,
		browser:           base.Browser,
		capabilities:      base.Capabilities,
		capabilitySources: base.CapabilitySources,
	}, os.Stdin, os.Stdout, os.Stderr)
}

func launchRoleEnvironment(o resolvedLaunch, base roles.Env, emitter *events.Emitter, note func(string, ...any)) (roles.Env, error) {
	// Identity and response style come from the profile; the agent's own
	// Plan/Act instructions are added on top and do not compete with it.
	identity, warnings, err := app.Compile(app.IdentityOptions{Preset: selectedProfilePreset(o.selectedProfile), Name: selectedProfileName(o.selectedProfile), Persona: selectedProfilePersona(o.selectedProfile), User: o.userName, System: o.systemPrompt, Post: o.postHistory, MaxTokens: o.maxTokens, ContextLimit: base.ContextLimit, Temperature: o.temperature})
	if err != nil {
		return roles.Env{}, err
	}
	for _, warning := range warnings {
		note("Identity warning: " + warning)
	}
	// What GEKTOR checks written files for. Secrets apply to every text
	// file whatever the language; gofmt is one rule among them and lives
	// outside internal/checks so that package stays language-neutral.
	rules := append(checks.Secrets(), rules.All()...)
	env := roles.Env{
		Identity:          app.IdentityPrompt(identity),
		IdentityName:      identity.Name,
		UserName:          o.userName,
		RefreshModelFacts: base.RefreshModelFacts,
		Client:            base.Client,
		WS:                base.WS,
		Procs:             base.Procs,
		MaxTokens:         o.maxTokens,
		Temperature:       o.temperature,
		ContextLimit:      base.ContextLimit,
		MaxSteps:          o.maxSteps,
		RunTimeout:        o.runTimeout,
		Policy:            buildPolicy(o.allow, o.deny),
		Vision:            base.Vision,
		VisionKnown:       base.VisionKnown,
		GateContext:       base.GateContext,
		BackendKind:       o.backendKind,
		Model:             base.Model,
		Stream:            streamEnabled(o.stream, o.outputMode, o.tui),
		MCPTools:          base.MCPTools,
		Browser:           base.Browser,
		Capabilities:      base.Capabilities,
		CapabilitySources: base.CapabilitySources,
		OnDelta:           func(chunk string) { fmt.Print(chunk) },
		OnStep:            stepPrinter(o.outputMode, emitter, o.logMax),
		OnToolFinished: func(callID string) {
			if emitter != nil {
				events.EmitToolFinished(emitter, callID)
			}
		},
		OnToolOutput: func(callID, text string) {
			if o.outputMode == "json" {
				events.EmitToolOutput(emitter, callID, text)
			} else {
				fmt.Print(text)
			}
		},
		OnResult: func(result agent.ToolResult) {
			if o.outputMode == "json" {
				events.EmitToolResultStatus(emitter, result.CallID, result.Output, result.Failed, result.Status)
			}
		},
		OnUsage: usagePrinter(o.outputMode, emitter),
		OnPrompt: func(report llm.ContextReport) {
			if o.outputMode == "json" {
				emitter.Emit("prompt_budget", map[string]any{"report": report})
				return
			}
			if report.Request == 1 && report.BackendTokens == 0 {
				note("startup context: ~%d tokens (%d tool schemas); estimates exclude backend template/tokenization", report.EstimatedTokens, len(report.Tools))
				for _, section := range report.Sections {
					if section.EstimatedTokens > 0 {
						note("  %s: ~%d", section.Name, section.EstimatedTokens)
					}
				}
			}
		},
		Rules:   rules,
		Checks:  checks.NewReporter(rules, findingSink(emitter, o.outputMode)),
		OnNudge: nudgePrinter(o.outputMode),
	}

	return env, nil
}
