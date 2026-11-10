package main

import (
	"errors"
	"flag"
	"path/filepath"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/tui"
	"github.com/keshon/gektor/internal/userdirs"
)

type launchRequest struct {
	args              []string
	preferences       config.Preferences
	preferencePath    string
	profile           *config.Profile
	options           *launchOptions
	history           []llm.Message
	draft             string
	images            []string
	drafts            tui.Drafts
	directory         string
	common            bool
	reassignWorkspace bool
}

func runApplication(args []string) error {
	if len(args) > 0 && args[0] == "mcp" {
		return runMCP(args[1:])
	}
	if len(args) > 0 && args[0] == "workflow" {
		return runWorkflow(args[1:])
	}
	if len(args) > 0 && args[0] == "auth" {
		return runAuth(args[1:])
	}
	options, err := parseLaunchOptions(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	path := userdirs.Sub("application.json")
	if !options.tui {
		return runConfigured(launchRequest{options: &options, preferencePath: path})
	}
	prefs, err := config.LoadPreferences(path)
	if err != nil {
		return err
	}
	request := initialTUILaunch(options, prefs, path)
	for {
		o, err := resolveLaunch(request)
		if err != nil {
			return err
		}
		request.preferences.Harness.MaxSteps = o.maxSteps
		request.preferences.Harness.DelegateSteps = o.delegateSteps
		request.preferences.Harness.DelegateMaxSteps = o.delegateMaxSteps
		err = runResolved(request, o)
		var change *tui.ModeChange
		if !errors.As(err, &change) {
			return err
		}
		prefs = change.Preferences
		if err := config.SavePreferences(path, prefs); err != nil {
			return err
		}
		next := relaunchOptions(o.launchOptions, prefs, change.KeepAct)
		request = launchRequest{options: &next, preferences: prefs, preferencePath: path, profile: prefs.Profile, history: change.History, draft: change.Draft, images: change.Images, drafts: change.Drafts, directory: change.Directory, reassignWorkspace: change.ReassignWorkspace, common: true}
	}
}

// Keep execution policy in memory. Settings owns the profile and harness;
// task-only arguments must not replay when opening another session.
func relaunchOptions(previous launchOptions, p config.Preferences, keepAct bool) launchOptions {
	previous.explicitFlags = map[string]bool{"browser-profile": previous.explicitFlags["browser-profile"]}
	previous.profilePath, previous.workspace = p.ProfilePath, p.Workspace
	previous.task, previous.resume, previous.fork, previous.images, previous.answer = "", "", "", "", ""
	previous.plan = !keepAct
	previous.maxSteps = 0
	previous.delegateSteps, previous.delegateMaxSteps = 0, 0
	return previous
}
func initialTUILaunch(options launchOptions, p config.Preferences, path string) launchRequest {
	request := launchRequest{preferences: p, preferencePath: path, common: true}
	// Runtime flags do not make settings temporary. Only explicit workspace or
	// profile values must be kept out of automatic preference writes.
	for _, name := range []string{"workspace", "profile", "backend-kind", "backend", "model", "api-key", "api-key-env", "max-tokens", "context-limit", "temperature", "reasoning-effort", "reasoning-summary", "user-name", "system-prompt", "post-history"} {
		if options.explicitFlags[name] {
			request.common = false
			break
		}
	}
	if !options.explicitFlags["profile"] {
		options.profilePath, request.profile = p.ProfilePath, p.Profile
		if request.profile == nil && options.profilePath == "" {
			profile := config.DefaultProfile()
			request.profile = &profile
		}
	}
	if !options.explicitFlags["workspace"] && p.Workspace != "" {
		options.workspace = p.Workspace
	}
	if !options.explicitFlags["plan"] {
		options.plan = p.Mode != "act"
	}
	request.options = &options
	return request
}

// Materialize the resolved settings so the editor and the runner agree.
func launchProfile(o resolvedLaunch, contextLimit int) config.Profile {
	generation := config.Generation{
		ReasoningEffort: o.reasoningEffort, ReasoningSummary: o.reasoningSummary,
		MaxTokens:    o.maxTokens,
		ContextLimit: contextLimit,
		Temperature:  o.temperature,
	}
	if generation.ContextLimit <= 0 {
		// This fallback is used only when the backend reports no window.
		// A detected window is preserved even below the response ceiling.
		generation.ContextLimit = max(4*generation.MaxTokens, 8192)
	}
	return config.Profile{
		Version: 1,
		Provider: config.Provider{
			Kind:      o.backendKind,
			URL:       o.backendURL,
			Model:     o.model,
			APIKeyEnv: o.apiKeyEnv,
		},
		Generation: generation,
		// Written rather than left nil for PrepareProfile to substitute:
		// the substitution never reached the profile, so the You page
		// opened empty against defaults nobody had chosen.
		Identity: &config.Identity{
			UserName:     o.userName,
			SystemPrompt: o.systemPrompt,
			PostHistory:  o.postHistory,
		},
	}
}

func applicationControl(launch launchRequest, p config.Profile, path string, workspace string) *tui.Application {
	if p.Version == 0 {
		return nil
	}
	prefs := launch.preferences
	prefs.Profile = &p
	if path != "" {
		if absolute, err := filepath.Abs(path); err == nil {
			path = absolute
		}
	}
	prefs.ProfilePath = path
	if absolute, err := filepath.Abs(workspace); err == nil {
		prefs.Workspace = absolute
	}
	return &tui.Application{Preferences: prefs, PreferencePath: launch.preferencePath, Temporary: !launch.common}
}

func selectedProfileName(p config.Profile) string {
	if p.Identity != nil {
		return p.Identity.Name
	}
	return ""
}
func selectedProfilePersona(p config.Profile) string {
	if p.Identity != nil {
		return p.Identity.UserPersona
	}
	return ""
}

func selectedProfilePreset(p config.Profile) string {
	if p.Identity != nil {
		return p.Identity.PromptPreset
	}
	return ""
}
