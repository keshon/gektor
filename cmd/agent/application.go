package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/speech"
	speechservice "github.com/keshon/tars/internal/speech/service"
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
		prefs.ProfilePath = filepath.Join(root, "profiles", "chat-veyra-local.json")
		if _, err := os.Stat(filepath.Join(root, "assets", "models", "thinking", "qwen", "Qwen3-1.7B-Q8_0.gguf")); err != nil {
			prefs.ProfilePath = filepath.Join(root, "profiles", "chat-veyra.json")
		}
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
		if prefs.Mode == "agent" {
			nextArgs = append(nextArgs, agentOptions...)
		}
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
	args := []string{"-tui", "-conversation", p.Mode, "-profile", p.ProfilePath}
	if p.Voice != "off" {
		args = append(args, "-voice")
	}
	if p.Mode == "agent" {
		args = append(args, "-workspace", p.Workspace, "-plan")
	}
	return args
}
func applicationControl(launch launchRequest, p config.Profile, path string, isChat bool, workspace string) *tui.Application {
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
	if !isChat {
		absolute, err := filepath.Abs(workspace)
		if err == nil {
			prefs.Workspace = absolute
		}
	}
	prefs.Mode = "agent"
	if isChat {
		prefs.Mode = "chat"
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

func configureAgentServices(profile config.Profile, directory string) (tui.ChatConfig, error) {
	out := tui.ChatConfig{Profile: profile, Directory: directory}
	if err := profile.Validate(); err != nil {
		return out, err
	}
	c := profile.Conversation
	if c == nil {
		c = &config.Conversation{UserName: "User", SystemPrompt: "You are a helpful conversational assistant."}
	}
	main, post := true, true
	if c.PreferCharacterPrompt != nil {
		main = *c.PreferCharacterPrompt
	}
	if c.PreferCharacterPostHistory != nil {
		post = *c.PreferCharacterPostHistory
	}
	var err error
	out.Settings, out.Warnings, err = compileChatSettings(chatOptions{Preset: c.PromptPreset, Name: c.Name, Persona: c.UserPersona, GreetingIndex: c.GreetingIndex, Character: c.Character, User: c.UserName, System: c.SystemPrompt, Post: c.PostHistory, PreferMain: main, PreferPost: post, MaxTokens: profile.Generation.MaxTokens, ContextLimit: profile.Generation.ContextLimit, Temperature: profile.Generation.Temperature})
	if err != nil {
		return out, err
	}
	if profile.Speech != nil {
		cfg := *profile.Speech
		out.NewVoice = func(ctx context.Context, dir string) (speech.Service, error) { return speechservice.New(ctx, cfg, dir) }
	}
	out.Configure = func(next config.Profile) (tui.ChatConfig, error) { return configureAgentServices(next, directory) }
	return out, nil
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
	if p.Conversation != nil {
		return p.Conversation.Name
	}
	return ""
}
func selectedProfilePersona(p config.Profile) string {
	if p.Conversation != nil {
		return p.Conversation.UserPersona
	}
	return ""
}
func selectedGreetingIndex(p config.Profile) int {
	if p.Conversation != nil {
		return p.Conversation.GreetingIndex
	}
	return 0
}

func selectedProfilePreset(p config.Profile) string {
	if p.Conversation != nil {
		return p.Conversation.PromptPreset
	}
	return ""
}
