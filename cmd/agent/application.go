package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/tui"
)

type launchRequest struct {
	args           []string
	preferences    config.Preferences
	preferencePath string
	profile        *config.Profile
	history        []llm.Message
	draft          string
	directory      string
	common         bool
}

func runApplication(args []string) error {
	transientKey := argumentValue(args, "api-key")
	agentOptions := retainedAgentOptions(args)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	root := filepath.Dir(exe)
	path := filepath.Join(root, "var", "application.json")
	hasTUI := false
	for _, arg := range args {
		if arg == "-tui" || arg == "-tui=true" {
			hasTUI = true
		}
	}
	if !hasTUI {
		return runConfigured(launchRequest{args: args, preferencePath: path})
	}
	prefs, err := config.LoadPreferences(path)
	if err != nil {
		return err
	}
	bare := len(args) == 1 && (args[0] == "-tui" || args[0] == "-tui=true")
	if prefs.ProfilePath == "" {
		prefs.ProfilePath = filepath.Join(root, "profiles", "llama-external.json")
	}
	request := launchRequest{args: args, preferences: prefs, preferencePath: path, common: bare}
	if bare {
		request.profile = prefs.Profile
		request.args = applicationArgs(prefs)
	}
	for {
		err := runConfigured(request)
		var change *tui.ModeChange
		if !errors.As(err, &change) {
			return err
		}
		prefs = change.Preferences
		if err := config.SavePreferences(path, prefs); err != nil {
			return err
		}
		nextArgs := applicationArgs(prefs)
		nextArgs = append(nextArgs, agentOptions...)
		if change.KeepAct {
			for i, arg := range nextArgs {
				if arg == "-plan" {
					nextArgs = append(nextArgs[:i], nextArgs[i+1:]...)
					break
				}
			}
		}
		if transientKey != "" {
			nextArgs = append(nextArgs, "-api-key", transientKey)
		}
		request = launchRequest{args: nextArgs, preferences: prefs, preferencePath: path, profile: prefs.Profile, history: change.History, draft: change.Draft, directory: change.Directory, common: true}
	}
}
func applicationArgs(p config.Preferences) []string {
	args := []string{"-tui"}
	// Only when there is a file. A generated setup has none, and emitting
	// an empty -profile produced a launch that parsed successfully and
	// then came up on the flag defaults.
	if p.ProfilePath != "" {
		args = append(args, "-profile", p.ProfilePath)
	}
	return append(args, "-workspace", p.Workspace, "-plan")
}
// launchProfile builds the profile a launch without -profile would have
// carried, so the plain `-tui` launch that docs/tui.md documents still
// has a working Settings menu.
//
// It exists because that menu reads the profile rather than the flags.
// With no profile the services handle and the Application both came back
// nil, and every row that depends on them was disabled with nothing on
// screen saying why — a launch that started fine and could not be
// configured at all.
//
// Values come from flags the launch already resolved, plus the context
// window the backend just reported, which is the same number the run
// itself uses. Identity is written rather than left nil, because the
// substitution ConfigureAgent does never reached the profile, and a
// generated session would then show an empty You page.
func launchProfile(o resolvedLaunch, contextLimit int) config.Profile {
	generation := config.Generation{
		MaxTokens:   o.maxTokens,
		ContextLimit: contextLimit,
		Temperature: o.temperature,
	}
	if generation.ContextLimit <= generation.MaxTokens {
		// The probe reported no window. Profile validation requires a limit
		// above the response budget, and erring small is the safe side of
		// that: history is compacted sooner instead of overrunning a
		// backend whose real window is unknown.
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
		// Written rather than left nil for ConfigureAgent to substitute:
		// the substitution never reached the profile, so the You page
		// opened empty against defaults nobody had chosen.
		Identity: &config.Identity{
			UserName:     config.DefaultUserName,
			SystemPrompt: config.DefaultSystemPrompt,
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

func argumentValue(args []string, name string) string {
	for i, arg := range args {
		if arg == "-"+name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "-"+name+"=") {
			return strings.TrimPrefix(arg, "-"+name+"=")
		}
	}
	return ""
}

// Preserve explicit execution policy for service/model handoffs in this process.
func retainedAgentOptions(args []string) []string {
	out := []string{}
	for _, name := range []string{"allow", "deny", "mcp", "reasoning-budget", "max-steps"} {
		value := argumentValue(args, name)
		if value != "" {
			out = append(out, "-"+name, value)
		}
	}
	for _, arg := range args {
		if arg == "-pure" || strings.HasPrefix(arg, "-pure=") {
			out = append(out, arg)
		}
	}
	return out
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
